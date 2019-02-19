package controller

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/fabric8-services/fabric8-common/errors"
	"github.com/fabric8-services/fabric8-common/log"
	"github.com/fabric8-services/fabric8-tenant/app"
	"github.com/fabric8-services/fabric8-tenant/auth"
	"github.com/fabric8-services/fabric8-tenant/cluster"
	"github.com/fabric8-services/fabric8-tenant/configuration"
	"github.com/fabric8-services/fabric8-tenant/environment"
	"github.com/fabric8-services/fabric8-tenant/jsonapi"
	"github.com/fabric8-services/fabric8-tenant/openshift"
	"github.com/fabric8-services/fabric8-tenant/tenant"
	"github.com/fabric8-services/fabric8-wit/rest"
	"github.com/goadesign/goa"
	errs "github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

// TenantController implements the tenant resource.
type TenantController struct {
	*goa.Controller
	config            *configuration.Data
	clusterService    cluster.Service
	authClientService auth.Service
	tenantService     tenant.Service
}

// NewTenantController creates a tenant controller.
func NewTenantController(
	service *goa.Service,
	tenantService tenant.Service,
	clusterService cluster.Service,
	authClientService auth.Service,
	config *configuration.Data) *TenantController {

	return &TenantController{
		Controller:        service.NewController("TenantController"),
		config:            config,
		clusterService:    clusterService,
		authClientService: authClientService,
		tenantService:     tenantService,
	}
}

type setupContext interface {
	context.Context
	Accepted() error
	BadRequest(r *app.JSONAPIErrors) error
	Unauthorized(r *app.JSONAPIErrors) error
	NotFound(r *app.JSONAPIErrors) error
	Conflict() error
	InternalServerError(r *app.JSONAPIErrors) error
}

// Clean runs the clean action.
func (c *TenantController) Clean(ctx *app.CleanTenantContext) error {
	// gets user info
	user, err := c.authClientService.GetUser(ctx)
	if err != nil {
		log.Error(ctx, map[string]interface{}{"err": err}, "creation of the user failed")
		return jsonapi.JSONErrorResponse(ctx, err)
	}

	tenantRepository := c.tenantService.NewTenantRepository(user.ID)
	// gets list of existing namespaces in DB
	namespaces, err := tenantRepository.GetNamespaces()
	if err != nil {
		log.Error(ctx, map[string]interface{}{
			"err":      err,
			"tenantID": user.ID,
		}, "retrieval of existing namespaces from DB failed")
		return jsonapi.JSONErrorResponse(ctx, err)
	}

	// get tenant entity
	dbTenant, err := c.getExistingTenant(ctx, user.ID, user.OpenShiftUsername)
	if err != nil {
		log.Error(ctx, map[string]interface{}{
			"err":      err,
			"tenantID": user.ID,
		}, "retrieval of tenant entity from DB failed")
		return jsonapi.JSONErrorResponse(ctx, errors.NewNotFoundError("tenant", user.ID.String()))
	}

	// checks if the namespaces should be only cleaned or totally removed - restrict deprovision from cluster to internal users only
	deleteOptions := openshift.DeleteOpts().EnableSelfHealing()
	if user.UserData.FeatureLevel != nil && *user.UserData.FeatureLevel == auth.InternalFeatureLevel && ctx.Remove {
		deleteOptions.RemoveFromCluster()
	}

	// create cluster mapping from existing namespaces
	clusterMapping, err := GetClusterMapping(ctx, c.clusterService, namespaces)
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, errors.NewInternalError(ctx, err))
	}

	// creates openshift services
	openShiftService := c.newOpenShiftService(ctx, user, dbTenant.NsBaseName, clusterMapping)

	// perform delete method on the list of existing namespaces
	err = openShiftService.Delete(environment.DefaultEnvTypes, namespaces, deleteOptions)
	if err != nil {
		namespaces, getErr := tenantRepository.GetNamespaces()
		if getErr != nil {
			log.Error(ctx, map[string]interface{}{
				"err":      err,
				"tenantID": user.ID,
			}, "retrieval of existing namespaces from DB after the removal attempt failed")
			return jsonapi.JSONErrorResponse(ctx, errs.Wrap(err, err.Error()))
		}
		params := namespacesToParams(namespaces)
		params["err"] = err
		log.Error(ctx, params, "deletion of namespaces failed")
		return jsonapi.JSONErrorResponse(ctx, err)
	}

	return ctx.NoContent()
}

func GetClusterMapping(ctx context.Context, clusterService cluster.Service, namespaces []*tenant.Namespace) (cluster.ForType, error) {
	clusterMapping := map[environment.Type]cluster.Cluster{}
	for _, namespace := range namespaces {
		// fetch the cluster info
		clustr, err := clusterService.GetCluster(ctx, namespace.MasterURL)
		if err != nil {
			log.Error(ctx, map[string]interface{}{
				"err":         err,
				"cluster_url": namespace.MasterURL,
			}, "unable to fetch cluster for user")
			return nil, err
		}
		clusterMapping[namespace.Type] = clustr
	}
	return cluster.ForTypeMapping(clusterMapping), nil
}

// Setup runs the setup action.
func (c *TenantController) Setup(ctx *app.SetupTenantContext) error {
	err := c.internalSetup(ctx)
	if ctx.ResponseData.Status == http.StatusAccepted {
		ctx.ResponseData.Header().Set("Location", rest.AbsoluteURL(ctx.RequestData.Request, app.TenantHref()))
	}
	return err
}

func (c *TenantController) SetupEnv(ctx *app.SetupEnvTenantContext) error {
	// convert to valid env type
	envType := environment.ToType(ctx.EnvType)
	if envType == "" {
		return errors.NewInternalErrorFromString(fmt.Sprintf("Environment type '%s' is either invalid or not supported", ctx.EnvType))
	}

	// setup env
	err := c.internalSetup(ctx, envType)
	if ctx.ResponseData.Status == http.StatusAccepted {
		ctx.ResponseData.Header().Set("Location", rest.AbsoluteURL(ctx.RequestData.Request, app.TenantHref()))
	}
	return err
}

func (c *TenantController) internalSetup(ctx setupContext, envTypes ...environment.Type) error {
	// gets user info
	user, err := c.authClientService.GetUser(ctx)
	if err != nil {
		log.Error(ctx, map[string]interface{}{"err": err}, "creation of the user failed")
		return jsonapi.JSONErrorResponse(ctx, err)
	}

	var dbTenant *tenant.Tenant
	var namespaces []*tenant.Namespace
	tenantRepository := c.tenantService.NewTenantRepository(user.ID)
	// check if tenant already exists
	if tenantRepository.Exists() {
		// if exists, then check existing namespace (if all of them are created or if any is missing)
		namespaces, err = tenantRepository.GetNamespaces()
		if err != nil {
			log.Error(ctx, map[string]interface{}{
				"err":      err,
				"tenantID": user.ID,
			}, "retrieval of existing namespaces from DB failed")
			return jsonapi.JSONErrorResponse(ctx, err)
		}
		dbTenant, err = c.getExistingTenant(ctx, user.ID, user.OpenShiftUsername)
		if err != nil {
			log.Error(ctx, map[string]interface{}{
				"err":      err,
				"tenantID": user.ID,
			}, "retrieval of tenant entity from DB failed")
			return jsonapi.JSONErrorResponse(ctx, errors.NewNotFoundError("tenant", user.ID.String()))
		}
	} else {
		nsBaseName, err := tenant.ConstructNsBaseName(c.tenantService, environment.RetrieveUserName(user.OpenShiftUsername))
		if err != nil {
			log.Error(ctx, map[string]interface{}{
				"err":         err,
				"os_username": user.OpenShiftUsername,
			}, "unable to construct namespace base name")
			return jsonapi.JSONErrorResponse(ctx, errors.NewInternalError(ctx, err))
		}
		// if does not exist then create a new tenant
		dbTenant = &tenant.Tenant{
			ID:         user.ID,
			Email:      *user.UserData.Email,
			OSUsername: user.OpenShiftUsername,
			NsBaseName: nsBaseName,
		}
		err = tenantRepository.CreateTenant(dbTenant)
		if err != nil {
			if strings.Contains(err.Error(), "pq: duplicate key value violates unique constraint") {
				return ctx.Conflict()
			}
			log.Error(ctx, map[string]interface{}{
				"err": err,
			}, "unable to store tenant configuration")
			return jsonapi.JSONErrorResponse(ctx, err)
		}
	}

	// check if any environment type is missing - should be provisioned
	var missing []environment.Type
	if len(envTypes) == 0 {
		missing, _ = filterMissingAndExisting(namespaces, environment.DefaultEnvTypes)
	} else {
		missing, _ = filterMissingAndExisting(namespaces, envTypes)
	}
	if len(missing) == 0 {
		return ctx.Conflict()
	}

	clusterNsMapping, err := c.clusterService.GetUserClusterForType(ctx, user)
	if err != nil {
		log.Error(ctx, map[string]interface{}{
			"err":         err,
			"tenant":      user.ID,
			"cluster_url": *user.UserData.Cluster,
		}, "unable to fetch cluster for tenant")
		return jsonapi.JSONErrorResponse(ctx, errors.NewInternalError(ctx, err))
	}

	// create openshift service
	service := c.newOpenShiftService(ctx, user, dbTenant.NsBaseName, clusterNsMapping)

	// perform post method on the list of missing environment types
	err = service.Create(missing, openshift.CreateOpts().EnableSelfHealing())
	if err != nil {
		log.Error(ctx, map[string]interface{}{
			"err":             err,
			"tenantID":        user.ID,
			"envTypeToCreate": missing,
		}, "creation of namespaces failed")
		return jsonapi.JSONErrorResponse(ctx, err)
	}

	return ctx.Accepted()
}

// Show runs the show action.
func (c *TenantController) Show(ctx *app.ShowTenantContext) error {
	// get user info
	user, err := c.authClientService.GetUser(ctx)
	if err != nil {
		log.Error(ctx, map[string]interface{}{"err": err}, "creation of the user failed")
		return jsonapi.JSONErrorResponse(ctx, err)
	}

	// gets tenant from DB
	tenant, err := c.getExistingTenant(ctx, user.ID, user.OpenShiftUsername)
	if err != nil {
		log.Error(ctx, map[string]interface{}{
			"err":      err,
			"tenantID": user.ID,
		}, "retrieval of tenant entity from DB failed")
		return jsonapi.JSONErrorResponse(ctx, errors.NewNotFoundError("tenants", user.ID.String()))
	}

	tenantRepository := c.tenantService.NewTenantRepository(user.ID)
	// gets tenant's namespaces
	namespaces, err := tenantRepository.GetNamespaces()
	if err != nil {
		log.Error(ctx, map[string]interface{}{
			"err":      err,
			"tenantID": user.ID,
		}, "retrieval of existing namespaces from DB failed")
		return jsonapi.JSONErrorResponse(ctx, err)
	}

	return ctx.OK(&app.TenantSingle{Data: convertTenant(ctx, tenant, namespaces, c.clusterService.GetCluster)})
}

// Update runs the update action.
func (c *TenantController) Update(ctx *app.UpdateTenantContext) error {
	// get user info
	user, err := c.authClientService.GetUser(ctx)
	if err != nil {
		log.Error(ctx, map[string]interface{}{"err": err}, "creation of the user failed")
		return jsonapi.JSONErrorResponse(ctx, err)
	}

	// getting tenant from DB
	dbTenant, err := c.getExistingTenant(ctx, user.ID, user.OpenShiftUsername)
	if err != nil {
		log.Error(ctx, map[string]interface{}{
			"err":      err,
			"tenantID": user.ID,
		}, "retrieval of tenant entity from DB failed")
		return jsonapi.JSONErrorResponse(ctx, errors.NewNotFoundError("tenant", user.ID.String()))
	}

	TenantUpdater{Config: c.config, ClusterService: c.clusterService, TenantService: c.tenantService}.
		Update(ctx, dbTenant, user, environment.DefaultEnvTypes, true)

	ctx.ResponseData.Header().Set("Location", rest.AbsoluteURL(ctx.RequestData.Request, app.TenantHref()))
	return ctx.Accepted()
}

type TenantUpdater struct {
	ClusterService cluster.Service
	TenantService  tenant.Service
	Config         *configuration.Data
}

func (u TenantUpdater) Update(ctx context.Context, dbTenant *tenant.Tenant, user *auth.User, envTypes []environment.Type, allowSelfHealing bool) error {
	tenantRepository := u.TenantService.NewTenantRepository(dbTenant.ID)
	// get tenant's namespaces
	namespaces, err := tenantRepository.GetNamespaces()
	if err != nil {
		log.Error(ctx, map[string]interface{}{
			"err":      err,
			"tenantID": dbTenant.ID,
		}, "retrieval of existing namespaces from DB failed")
		return jsonapi.JSONErrorResponse(ctx, err)
	}

	// create cluster mapping from existing namespaces
	clusterMapping, err := GetClusterMapping(ctx, u.ClusterService, namespaces)
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, errors.NewInternalError(ctx, err))
	}

	// create openshift service
	nsRepo := u.TenantService.NewTenantRepository(dbTenant.ID)

	var envService *environment.Service
	var userTokenResolver openshift.UserTokenResolver
	if user != nil {
		envService = environment.NewServiceForUserData(user.UserData)
		userTokenResolver = openshift.TokenResolverForUser(user)
	} else {
		envService = environment.NewService()
		userTokenResolver = openshift.TokenResolver()
	}

	serviceContext := openshift.NewServiceContext(
		ctx, u.Config, clusterMapping, dbTenant.OSUsername, dbTenant.NsBaseName, userTokenResolver)
	openShiftService := openshift.NewService(serviceContext, nsRepo, envService)

	// perform patch method on the list of exiting namespaces
	err = openShiftService.Update(envTypes, namespaces, openshift.UpdateOpts().EnableSelfHealing())
	if err != nil {
		log.Error(ctx, map[string]interface{}{
			"err":                err,
			"tenantID":           dbTenant.ID,
			"namespacesToUpdate": listNames(namespaces),
		}, "update of namespaces failed")
		return jsonapi.JSONErrorResponse(ctx, err)
	}
	return nil
}

func listNames(namespaces []*tenant.Namespace) []string {
	var names []string
	for _, ns := range namespaces {
		names = append(names, ns.Name)
	}
	return names
}

func (c *TenantController) getExistingTenant(ctx context.Context, id uuid.UUID, osUsername string) (*tenant.Tenant, error) {
	dbTenant, err := c.tenantService.NewTenantRepository(id).GetTenant()
	if err != nil {
		return nil, err
	}
	if dbTenant.NsBaseName == "" {
		dbTenant.NsBaseName = environment.RetrieveUserName(osUsername)
	}
	return dbTenant, nil
}

func (c *TenantController) newOpenShiftService(ctx context.Context, user *auth.User, nsBaseName string, clusterNsMapping cluster.ForType) *openshift.ServiceBuilder {
	nsRepo := c.tenantService.NewTenantRepository(user.ID)

	envService := environment.NewServiceForUserData(user.UserData)

	serviceContext := openshift.NewServiceContext(
		ctx, c.config, clusterNsMapping, user.OpenShiftUsername, nsBaseName, openshift.TokenResolverForUser(user))
	return openshift.NewService(serviceContext, nsRepo, envService)
}

func filterMissingAndExisting(namespaces []*tenant.Namespace, searchEnvTypes []environment.Type) (missing []environment.Type, existing []environment.Type) {
	exitingTypes := GetNamespaceByType(namespaces)

	missingNamespaces := make([]environment.Type, 0)
	existingNamespaces := make([]environment.Type, 0)
	for _, nsType := range searchEnvTypes {
		_, exits := exitingTypes[nsType]
		if !exits {
			missingNamespaces = append(missingNamespaces, nsType)
		} else {
			existingNamespaces = append(existingNamespaces, nsType)
		}
	}
	return missingNamespaces, existingNamespaces
}

func GetNamespaceByType(namespaces []*tenant.Namespace) map[environment.Type]*tenant.Namespace {
	nsTypes := map[environment.Type]*tenant.Namespace{}
	for _, namespace := range namespaces {
		nsTypes[namespace.Type] = namespace
	}
	return nsTypes
}
