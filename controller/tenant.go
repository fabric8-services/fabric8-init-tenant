package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/fabric8-services/fabric8-tenant/app"
	"github.com/fabric8-services/fabric8-tenant/auth"
	"github.com/fabric8-services/fabric8-tenant/jsonapi"
	"github.com/fabric8-services/fabric8-tenant/openshift"
	"github.com/fabric8-services/fabric8-tenant/tenant"
	"github.com/fabric8-services/fabric8-tenant/token"
	"github.com/fabric8-services/fabric8-wit/errors"
	"github.com/fabric8-services/fabric8-wit/log"
	"github.com/fabric8-services/fabric8-wit/rest"
	"github.com/goadesign/goa"
	goajwt "github.com/goadesign/goa/middleware/security/jwt"
	uuid "github.com/satori/go.uuid"
)

// TenantController implements the status resource.
type TenantController struct {
	*goa.Controller
	tenantService            tenant.Service
	userService              token.UserService
	tokenManager             token.Manager
	defaultOpenshiftTemplate openshift.Config
	templateVars             map[string]string
}

// NewTenantController creates a status controller.
func NewTenantController(
	service *goa.Service,
	tenantService tenant.Service,
	userService token.UserService,
	tokenManager token.Manager,
	defaultOpenshiftTemplate openshift.Config,
	templateVars map[string]string) *TenantController {

	return &TenantController{
		Controller:               service.NewController("TenantController"),
		tenantService:            tenantService,
		userService:              userService,
		tokenManager:             tokenManager,
		defaultOpenshiftTemplate: defaultOpenshiftTemplate,
		templateVars:             templateVars,
	}
}

// Setup runs the setup action.
func (c *TenantController) Setup(ctx *app.SetupTenantContext) error {
	userToken := goajwt.ContextJWT(ctx)
	if userToken == nil {
		return jsonapi.JSONErrorResponse(ctx, errors.NewUnauthorizedError("Missing JWT token"))
	}
	ttoken := &TenantToken{token: userToken}
	exists := c.tenantService.Exists(ttoken.Subject())
	if exists {
		return ctx.Conflict()
	}

	// fetch the cluster the user belongs to
	user, err := c.userService.CurrentUser(ctx, userToken.Raw)
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, err)
	}

	// fetch the users cluster token
	openshiftUsername, openshiftUserToken, err := c.tokenManager.Tenant(ctx, *user.Cluster, userToken.Raw)
	if err != nil {
		log.Error(ctx, map[string]interface{}{
			"err": err,
		}, "unable to fetch tenant token from auth")
		return jsonapi.JSONErrorResponse(ctx, errors.NewUnauthorizedError("Could not authorization against keycloak"))
	}

	// create openshift config
	openshiftConfig, err := usersOpenshiftConfig(ctx, user, c.defaultOpenshiftTemplate, c.tokenManager)
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, errors.NewInternalError(ctx, err))
	}

	// setup possible version overides
	openshiftConfig = overrideTemplateVersions(user, openshiftConfig)

	tenant := &tenant.Tenant{ID: ttoken.Subject(), Email: ttoken.Email()}
	err = c.tenantService.SaveTenant(tenant)
	if err != nil {
		log.Error(ctx, map[string]interface{}{
			"err": err,
		}, "unable to store tenant configuration")
		return jsonapi.JSONErrorResponse(ctx, errors.NewInternalError(ctx, err))
	}

	go func() {
		ctx := ctx
		t := tenant
		err = openshift.RawInitTenant(
			ctx,
			openshiftConfig,
			InitTenant(ctx, openshiftConfig.MasterURL, c.tenantService, t),
			openshiftUsername,
			openshiftUserToken,
			c.templateVars)

		if err != nil {
			log.Error(ctx, map[string]interface{}{
				"err":     err,
				"os_user": openshiftUsername,
			}, "unable initialize tenant")
		}
	}()

	ctx.ResponseData.Header().Set("Location", rest.AbsoluteURL(ctx.RequestData.Request, app.TenantHref()))
	return ctx.Accepted()
}

// Update runs the setup action.
func (c *TenantController) Update(ctx *app.UpdateTenantContext) error {
	userToken := goajwt.ContextJWT(ctx)
	if userToken == nil {
		return jsonapi.JSONErrorResponse(ctx, errors.NewUnauthorizedError("Missing JWT token"))
	}
	ttoken := &TenantToken{token: userToken}
	tenant, err := c.tenantService.GetTenant(ttoken.Subject())
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, errors.NewNotFoundError("tenants", ttoken.Subject().String()))
	}

	// fetch the cluster the user belongs to
	user, err := c.userService.CurrentUser(ctx, userToken.Raw)
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, err)
	}

	// fetch the users cluster token
	openshiftUsername, _, err := c.tokenManager.Tenant(ctx, *user.Cluster, userToken.Raw)
	if err != nil {
		log.Error(ctx, map[string]interface{}{
			"err": err,
		}, "unable to fetch tenant token from auth")
		return jsonapi.JSONErrorResponse(ctx, errors.NewUnauthorizedError("No token found"))
	}

	// create openshift config
	openshiftConfig, err := usersOpenshiftConfig(ctx, user, c.defaultOpenshiftTemplate, c.tokenManager)
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, errors.NewInternalError(ctx, err))
	}

	// setup possible version overides
	openshiftConfig = overrideTemplateVersions(user, openshiftConfig)

	go func() {
		ctx := ctx
		t := tenant
		err = openshift.RawUpdateTenant(
			ctx,
			openshiftConfig,
			InitTenant(ctx, openshiftConfig.MasterURL, c.tenantService, t),
			openshiftUsername,
			c.templateVars)

		if err != nil {
			log.Error(ctx, map[string]interface{}{
				"err":     err,
				"os_user": openshiftUsername,
			}, "unable initialize tenant")
		}
	}()

	ctx.ResponseData.Header().Set("Location", rest.AbsoluteURL(ctx.RequestData.Request, app.TenantHref()))
	return ctx.Accepted()
}

// Clean runs the setup action for the tenant namespaces.
func (c *TenantController) Clean(ctx *app.CleanTenantContext) error {
	userToken := goajwt.ContextJWT(ctx)
	if userToken == nil {
		return jsonapi.JSONErrorResponse(ctx, errors.NewUnauthorizedError("Missing JWT token"))
	}

	// fetch the cluster the user belongs to
	user, err := c.userService.CurrentUser(ctx, userToken.Raw)
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, err)
	}

	// fetch the users cluster token
	openshiftUsername, _, err := c.tokenManager.Tenant(ctx, *user.Cluster, userToken.Raw)
	if err != nil {
		log.Error(ctx, map[string]interface{}{
			"err": err,
		}, "unable to fetch tenant token from auth")
		return jsonapi.JSONErrorResponse(ctx, errors.NewUnauthorizedError("No token found"))
	}

	// create openshift config
	openshiftConfig, err := usersOpenshiftConfig(ctx, user, c.defaultOpenshiftTemplate, c.tokenManager)
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, errors.NewInternalError(ctx, err))
	}

	err = openshift.CleanTenant(ctx, openshiftConfig, openshiftUsername, c.templateVars)
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, err)
	}
	// TODO (xcoulon): respond with `204 No Content` instead ?
	return ctx.OK([]byte{})
}

// Show runs the setup action.
func (c *TenantController) Show(ctx *app.ShowTenantContext) error {
	token := goajwt.ContextJWT(ctx)
	if token == nil {
		return jsonapi.JSONErrorResponse(ctx, errors.NewUnauthorizedError("Missing JWT token"))
	}

	ttoken := &TenantToken{token: token}
	tenantID := ttoken.Subject()
	tenant, err := c.tenantService.GetTenant(tenantID)
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, errors.NewNotFoundError("tenants", tenantID.String()))
	}

	namespaces, err := c.tenantService.GetNamespaces(tenantID)
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, err)
	}
	result := &app.TenantSingle{Data: convertTenant(tenant, namespaces)}
	return ctx.OK(result)
}

// usersOpenshiftConfig builds openshift config for every user request depending on the user profile
func usersOpenshiftConfig(ctx context.Context, user *auth.UserDataAttributes, osTemplate openshift.Config, tokenManager token.Manager) (openshift.Config, error) {

	// fetch ClusterToken of the cluster that user belongs to
	clusterUser, clusterToken, err := tokenManager.Cluster(ctx, *user.Cluster)
	if err != nil {
		return openshift.Config{}, err
	}

	return osTemplate.WithMasterUser(clusterUser).WithToken(clusterToken).WithMasterURL(*user.Cluster), nil
}

func overrideTemplateVersions(user *auth.UserDataAttributes, config openshift.Config) openshift.Config {
	userContext := user.ContextInformation
	if tc, found := userContext["tenantConfig"]; found {
		if tenantConfig, ok := tc.(map[string]interface{}); ok {
			find := func(key, defaultValue string) string {
				if rawValue, found := tenantConfig[key]; found {
					if value, ok := rawValue.(string); ok {
						return value
					}
				}
				return defaultValue
			}

			return config.WithUserSettings(
				find("cheVersion", config.CheVersion),
				find("jenkinsVersion", config.JenkinsVersion),
				find("teamVersion", config.TeamVersion),
				find("mavenRepo", config.MavenRepoURL),
			)
		}
	}
	return config
}

//TODO: Remove WhoAmI
func (c *TenantController) WhoAmI(token *jwt.Token, openshiftUserToken string) (string, error) {
	//return OpenShiftWhoAmI(token, c.openshiftConfig, openshiftUserToken)
	return "", nil
}

func OpenShiftWhoAmI(token *jwt.Token, oc openshift.Config, openshiftUserToken string) (string, error) {
	if openshift.KubernetesMode() {
		// We don't currently store the Kubernetes token into KeyCloak for now
		// so lets try load the token for the ServiceAccount for the KeyCloak username
		// or lazily create a ServiceAccount if there is none created yet
		ttoken := &TenantToken{token: token}
		userName := ttoken.Username()
		if len(userName) == 0 {
			return "", fmt.Errorf("No username or preferred_username associated with the JWT token!")
		}
		return userName, nil
	}
	return openshift.WhoAmI(oc.WithToken(openshiftUserToken))
}

// InitTenant is a Callback that assumes a new tenant is being created
func InitTenant(ctx context.Context, masterURL string, service tenant.Service, currentTenant *tenant.Tenant) openshift.Callback {
	return func(statusCode int, method string, request, response map[interface{}]interface{}) (string, map[interface{}]interface{}) {
		log.Info(ctx, map[string]interface{}{
			"status":    statusCode,
			"method":    method,
			"namespace": openshift.GetNamespace(request),
			"name":      openshift.GetName(request),
			"kind":      openshift.GetKind(request),
		}, "resource requested")
		if statusCode == http.StatusConflict {
			if openshift.GetKind(request) == openshift.ValKindNamespace {
				return "", nil
			}
			if openshift.GetKind(request) == openshift.ValKindProjectRequest {
				return "", nil
			}
			if openshift.GetKind(request) == openshift.ValKindPersistenceVolumeClaim {
				return "", nil
			}
			if openshift.GetKind(request) == openshift.ValKindServiceAccount {
				return "", nil
			}
			return "DELETE", request
		} else if statusCode == http.StatusCreated {
			if openshift.GetKind(request) == openshift.ValKindProjectRequest {
				name := openshift.GetName(request)
				service.SaveNamespace(&tenant.Namespace{
					TenantID:  currentTenant.ID,
					Name:      name,
					State:     "created",
					Version:   openshift.GetLabelVersion(request),
					Type:      tenant.GetNamespaceType(name),
					MasterURL: masterURL,
				})

				// HACK to workaround osio applying some dsaas-user permissions async
				// Should loop on a Check if allowed type of call instead
				time.Sleep(time.Second * 2)

			} else if openshift.GetKind(request) == openshift.ValKindNamespace {
				name := openshift.GetName(request)
				service.SaveNamespace(&tenant.Namespace{
					TenantID:  currentTenant.ID,
					Name:      name,
					State:     "created",
					Version:   openshift.GetLabelVersion(request),
					Type:      tenant.GetNamespaceType(name),
					MasterURL: masterURL,
				})
			}
			return "", nil
		} else if statusCode == http.StatusOK {
			if method == "DELETE" {
				return "POST", request
			}
			return "", nil
		}
		log.Info(ctx, map[string]interface{}{
			"status":    statusCode,
			"method":    method,
			"namespace": openshift.GetNamespace(request),
			"name":      openshift.GetName(request),
			"kind":      openshift.GetKind(request),
			"request":   request,
			"response":  response,
		}, "unhandled resource response")
		return "", nil
	}
}

func OpenshiftToken(openshiftConfig openshift.Config, token *jwt.Token) (string, error) {
	if openshift.KubernetesMode() {
		// We don't currently store the Kubernetes token into KeyCloak for now
		// so lets try load the token for the ServiceAccount for the KeyCloak username
		// or lazily create a ServiceAccount if there is none created yet
		ttoken := &TenantToken{token: token}
		kcUserName := ttoken.Username()
		return openshift.GetOrCreateKubeToken(openshiftConfig, kcUserName)
	}
	return "", nil
}

type TenantToken struct {
	token *jwt.Token
}

func (t TenantToken) Subject() uuid.UUID {
	if claims, ok := t.token.Claims.(jwt.MapClaims); ok {
		id, err := uuid.FromString(claims["sub"].(string))
		if err != nil {
			return uuid.UUID{}
		}
		return id
	}
	return uuid.UUID{}
}

func (t TenantToken) Username() string {
	if claims, ok := t.token.Claims.(jwt.MapClaims); ok {
		answer := claims["preferred_username"].(string)
		if len(answer) == 0 {
			answer = claims["username"].(string)
		}
		return answer
	}
	return ""
}

func (t TenantToken) Email() string {
	if claims, ok := t.token.Claims.(jwt.MapClaims); ok {
		return claims["email"].(string)
	}
	return ""
}

func convertTenant(tenant *tenant.Tenant, namespaces []*tenant.Namespace) *app.Tenant {
	result := app.Tenant{
		ID:   &tenant.ID,
		Type: "tenants",
		Attributes: &app.TenantAttributes{
			CreatedAt:  &tenant.CreatedAt,
			Email:      &tenant.Email,
			Profile:    &tenant.Profile,
			Namespaces: []*app.NamespaceAttributes{},
		},
	}
	for _, ns := range namespaces {
		tenantType := string(ns.Type)
		result.Attributes.Namespaces = append(
			result.Attributes.Namespaces,
			&app.NamespaceAttributes{
				CreatedAt:  &ns.CreatedAt,
				UpdatedAt:  &ns.UpdatedAt,
				ClusterURL: &ns.MasterURL,
				Name:       &ns.Name,
				Type:       &tenantType,
				Version:    &ns.Version,
				State:      &ns.State,
			})
	}
	return &result
}
