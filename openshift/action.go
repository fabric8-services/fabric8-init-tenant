package openshift

import (
	"context"
	"fmt"
	"github.com/fabric8-services/fabric8-common/log"
	"github.com/fabric8-services/fabric8-tenant/cluster"
	"github.com/fabric8-services/fabric8-tenant/environment"
	"github.com/fabric8-services/fabric8-tenant/sentry"
	"github.com/fabric8-services/fabric8-tenant/tenant"
	"github.com/fabric8-services/fabric8-tenant/utils"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
	"net/http"
	"sort"
)

// NamespaceAction represents the action that should be applied on the namespaces for the particular tenant - [post|update|delete].
// It is mainly responsible for operation on DB and provides additional information specific to the action that is needed by other objects
type NamespaceAction interface {
	MethodName() string
	GetNamespaceEntity(nsTypeService EnvironmentTypeService) (*tenant.Namespace, error)
	UpdateNamespace(env *environment.EnvData, cluster *cluster.Cluster, namespace *tenant.Namespace, failed bool)
	GetOperationSets(envService EnvironmentTypeService, client Client) (*environment.EnvData, []OperationSet, error)
	ForceMasterTokenGlobally() bool
	HealingStrategy() HealingFuncGenerator
	ManageAndUpdateResults(errorChan chan error, envTypes []environment.Type, healing Healing) error
}

type ActionOptions struct {
	allowSelfHealing bool
}

func (o *ActionOptions) EnableSelfHealing() *ActionOptions {
	o.allowSelfHealing = true
	return o
}

func (o *ActionOptions) DisableSelfHealing() *ActionOptions {
	o.allowSelfHealing = false
	return o
}

type DeleteActionOption struct {
	*ActionOptions
	removeFromCluster bool
	keepTenant        bool
}

func (o *DeleteActionOption) EnableSelfHealing() *DeleteActionOption {
	o.ActionOptions.EnableSelfHealing()
	return o
}

func (o *DeleteActionOption) DisableSelfHealing() *DeleteActionOption {
	o.ActionOptions.DisableSelfHealing()
	return o
}

func (o *DeleteActionOption) RemoveFromCluster() *DeleteActionOption {
	o.removeFromCluster = true
	o.keepTenant = false
	return o
}

func (o *DeleteActionOption) ButKeepTenantEntity() *DeleteActionOption {
	o.keepTenant = true
	return o
}

func CreateOpts() *ActionOptions {
	return &ActionOptions{allowSelfHealing: false}
}
func UpdateOpts() *ActionOptions {
	return &ActionOptions{allowSelfHealing: false}
}
func DeleteOpts() *DeleteActionOption {
	return &DeleteActionOption{ActionOptions: &ActionOptions{allowSelfHealing: false}, removeFromCluster: false, keepTenant: true}
}

type HealingFuncGenerator func(openShiftService *ServiceBuilder) Healing
type Healing func(originalError error) error

type commonNamespaceAction struct {
	method        string
	actionOptions *ActionOptions
	tenantRepo    tenant.Repository
	filterFunc    FilterFunc
	requestCtx    context.Context
}

func (c *commonNamespaceAction) MethodName() string {
	return c.method
}

func (c *commonNamespaceAction) getOperationSets(envService EnvironmentTypeService, client Client) (*EnvAndObjectsManager, []OperationSet, error) {
	objectManager, err := envService.GetEnvDataAndObjects()
	if err != nil {
		return objectManager, nil, errors.Wrap(err, "getting environment data and objects failed")
	}

	operationSets := []OperationSet{NewOperationSet(c.method, objectManager.GetObjects(c.filterFunc))}
	object, shouldBeAdded := envService.AdditionalObject()
	if len(object) > 0 {
		action := c.method
		if !shouldBeAdded {
			action = http.MethodDelete
		}
		if action == c.method {
			operationSets[0].Objects = append(operationSets[0].Objects, object)
		} else {
			operationSets = append(operationSets, NewOperationSet(action, []environment.Object{object}))
		}
	}

	sort.Sort(environment.ByKind(operationSets[0].Objects))
	return objectManager, operationSets, nil
}

func (c *commonNamespaceAction) ForceMasterTokenGlobally() bool {
	return true
}

var NoHealing = func(openShiftService *ServiceBuilder) Healing {
	return func(originalError error) error {
		return originalError
	}
}

func (c *commonNamespaceAction) HealingStrategy() HealingFuncGenerator {
	return NoHealing
}

func (c *commonNamespaceAction) ManageAndUpdateResults(errorChan chan error, envTypes []environment.Type, healing Healing) error {
	msg := utils.ListErrorsInMessage(errorChan, 100)
	if len(msg) > 0 {
		err := fmt.Errorf("%s method applied to namespace types %s failed with one or more errors:%s", c.method, envTypes, msg)
		if !c.actionOptions.allowSelfHealing {
			return err
		}
		return healing(err)
	}
	return nil
}

func (c *CreateAction) HealingStrategy() HealingFuncGenerator {
	return func(openShiftService *ServiceBuilder) Healing {
		return func(originalError error) error {
			log.Error(openShiftService.service.context.requestCtx, map[string]interface{}{
				"err":                   originalError,
				"self-healing-strategy": "recreate-with-new-nsBaseName",
			}, "the creation failed, starting self-healing logic")
			openShiftUsername := openShiftService.service.context.openShiftUsername
			tnnt, err := c.tenantRepo.GetTenant()
			errMsgSuffix := fmt.Sprintf("while doing self-healing operations triggered by error: [%s]", originalError)
			if err != nil {
				return errors.Wrapf(err, "unable to get tenant %s", errMsgSuffix)
			}
			namespaces, err := c.tenantRepo.GetNamespaces()
			if err != nil {
				return errors.Wrapf(err, "unable to get namespaces of tenant %s %s", tnnt.ID, errMsgSuffix)
			}
			err = openShiftService.Delete(environment.DefaultEnvTypes, namespaces, DeleteOpts().EnableSelfHealing().RemoveFromCluster().ButKeepTenantEntity())
			if err != nil {
				return errors.Wrapf(err, "deletion of namespaces failed %s", errMsgSuffix)
			}
			newNsBaseName, err := tenant.ConstructNsBaseName(c.tenantRepo, environment.RetrieveUserName(openShiftUsername))
			if err != nil {
				return errors.Wrapf(err, "unable to construct namespace base name for user with OSname %s %s", openShiftUsername, errMsgSuffix)
			}
			tnnt.NsBaseName = newNsBaseName
			err = c.tenantRepo.SaveTenant(tnnt)
			if err != nil {
				return errors.Wrapf(err, "unable to update tenant db entity %s", errMsgSuffix)
			}
			openShiftService.service.context.nsBaseName = newNsBaseName
			err = openShiftService.Create(environment.DefaultEnvTypes, CreateOpts().DisableSelfHealing())
			if err != nil {
				return errors.Wrapf(err, "unable to create new namespaces %s", errMsgSuffix)
			}
			return nil
		}
	}
}

func NewCreateAction(tenantRepo tenant.Repository, requestCtx context.Context, actionOpts *ActionOptions) *CreateAction {
	return &CreateAction{
		commonNamespaceAction: &commonNamespaceAction{
			method:        http.MethodPost,
			tenantRepo:    tenantRepo,
			actionOptions: actionOpts,
			requestCtx:    requestCtx,
			filterFunc: func(objects environment.Object) bool {
				return true
			},
		},
	}
}

type CreateAction struct {
	*commonNamespaceAction
}

func (c *CreateAction) GetNamespaceEntity(nsTypeService EnvironmentTypeService) (*tenant.Namespace, error) {
	namespace := c.tenantRepo.NewNamespace(
		nsTypeService.GetType(), nsTypeService.GetNamespaceName(), nsTypeService.GetCluster().APIURL, tenant.Provisioning)
	return c.tenantRepo.CreateNamespace(namespace)
}

func (c *CreateAction) UpdateNamespace(env *environment.EnvData, cluster *cluster.Cluster, namespace *tenant.Namespace, failed bool) {
	state := tenant.Ready
	namespace.Version = env.Version()
	if failed {
		state = tenant.Failed
	}
	namespace.UpdateData(env, cluster, state)
	err := c.tenantRepo.SaveNamespace(namespace)
	if err != nil {
		sentry.LogError(c.requestCtx, map[string]interface{}{
			"env_type": env.EnvType,
			"cluster":  cluster.APIURL,
			"tenant":   namespace.TenantID,
			"state":    state,
		}, err, "creation of namespace entity failed")
	}
}

func (c *CreateAction) ForceMasterTokenGlobally() bool {
	return false
}

func (c *CreateAction) GetOperationSets(envService EnvironmentTypeService, client Client) (*environment.EnvData, []OperationSet, error) {
	envAndObjectsManager, sets, err := c.getOperationSets(envService, client)
	if err != nil {
		return nil, sets, err
	}
	return envAndObjectsManager.EnvData, sets, nil
}

func NewDeleteAction(tenantRepo tenant.Repository, requestCtx context.Context, existingNamespaces []*tenant.Namespace, deleteOpts *DeleteActionOption) *DeleteAction {
	filterFunc := isOfKind(AllToGetAndDelete...)
	if deleteOpts.removeFromCluster {
		filterFunc = isOfKind(environment.ValKindProjectRequest)
	}

	return &DeleteAction{
		withExistingNamespacesAction: &withExistingNamespacesAction{
			commonNamespaceAction: &commonNamespaceAction{
				method:        http.MethodDelete,
				tenantRepo:    tenantRepo,
				actionOptions: deleteOpts.ActionOptions,
				requestCtx:    requestCtx,
				filterFunc:    filterFunc,
			},
			existingNamespaces: existingNamespaces,
		},
		deleteOptions: deleteOpts,
	}
}

type DeleteAction struct {
	*withExistingNamespacesAction
	deleteOptions *DeleteActionOption
}

func (d *DeleteAction) GetNamespaceEntity(nsTypeService EnvironmentTypeService) (*tenant.Namespace, error) {
	return d.getNamespaceFor(nsTypeService.GetType()), nil
}

func (d *DeleteAction) UpdateNamespace(env *environment.EnvData, cluster *cluster.Cluster, namespace *tenant.Namespace, failed bool) {
	var err error
	if failed {
		namespace.State = tenant.Failed
		err = d.tenantRepo.SaveNamespace(namespace)
	} else if d.deleteOptions.removeFromCluster {
		err = d.tenantRepo.DeleteNamespace(namespace)
	}
	if err != nil {
		sentry.LogError(d.requestCtx, map[string]interface{}{
			"env_type":            env.EnvType,
			"cluster":             cluster.APIURL,
			"tenant":              namespace.TenantID,
			"state":               namespace.State,
			"remove_from_cluster": d.deleteOptions.removeFromCluster,
		}, err, "deleting namespace entity failed")
	}
}

var AllToGetAndDelete = []string{environment.ValKindService, environment.ValKindPod, environment.ValKindReplicationController, environment.ValKindDaemonSet,
	environment.ValKindDeployment, environment.ValKindReplicaSet, environment.ValKindStatefulSet, environment.ValKindJob,
	environment.ValKindHorizontalPodAutoScaler, environment.ValKindCronJob, environment.ValKindDeploymentConfig,
	environment.ValKindBuildConfig, environment.ValKindBuild, environment.ValKindImageStream, environment.ValKindRoute,
	environment.ValKindPersistentVolumeClaim, environment.ValKindConfigMap}

func (d *DeleteAction) GetOperationSets(envService EnvironmentTypeService, client Client) (*environment.EnvData, []OperationSet, error) {
	objectManager, err := envService.GetEnvDataAndObjects()
	if err != nil {
		return objectManager.EnvData, nil, errors.Wrap(err, "getting environment data and objects failed")
	}
	toDelete := objectManager.GetObjects(d.filterFunc)
	var operationSets []OperationSet
	if !d.deleteOptions.removeFromCluster {
		var err error
		toDelete, err = getCleanObjects(client, envService.GetNamespaceName())
		if err != nil {
			return objectManager.EnvData, nil, err
		}
		operationSets = append(operationSets, NewOperationSet(http.MethodDelete, toDelete))
		operationSets = append(operationSets, NewOperationSet(EnsureDeletion, toDelete))
	} else {
		operationSets = append(operationSets, NewOperationSet(http.MethodDelete, toDelete))
	}

	sort.Sort(sort.Reverse(environment.ByKind(toDelete)))
	return objectManager.EnvData, operationSets, nil
}

func getCleanObjects(client Client, namespaceName string) (environment.Objects, error) {
	toClean := make(environment.Objects, 0)
	for _, kind := range AllToGetAndDelete {
		kindToGet := NewObject(kind, namespaceName, "")
		result, err := Apply(client, http.MethodGet, kindToGet)
		if err != nil {
			if result != nil && result.Response != nil {
				code := result.Response.StatusCode
				if code == http.StatusNotFound || code == http.StatusForbidden {
					log.Error(nil, map[string]interface{}{
						"kind":          kind,
						"namespaceName": namespaceName,
						"err":           err,
					}, "unable to get list of current objects. it is possible that there is no such object kind available")
					continue
				}
			}
			return nil, errors.Wrapf(err, "unable to get list of current objects of kind %s in namespace %s", kind, namespaceName)
		}
		var returnedObj environment.Object
		err = yaml.Unmarshal(result.Body, &returnedObj)
		if err != nil {
			return nil, errors.Wrapf(err,
				"unable unmarshal object responded from OS while getting list of current objects of kind %s in namespace %s", kind, namespaceName)
		}

		if items, itemsFound := returnedObj["items"]; itemsFound {
			if objects, isSlice := items.([]interface{}); isSlice && len(objects) > 0 {
				for _, obj := range objects {
					if object, isObj := obj.(environment.Object); isObj {
						if name := environment.GetName(object); name != "" {
							toClean = append(toClean, NewObject(kind, namespaceName, name))
						}
					}
				}
			}
		}
	}
	return toClean, nil
}

func NewObject(kind, namespaceName string, name string) environment.Object {
	return environment.Object{
		"kind": kind,
		"metadata": environment.Object{
			"namespace": namespaceName,
			"name":      name,
		},
	}
}

type withExistingNamespacesAction struct {
	*commonNamespaceAction
	existingNamespaces []*tenant.Namespace
}

func (a withExistingNamespacesAction) getNamespaceFor(nsType environment.Type) *tenant.Namespace {
	for _, ns := range a.existingNamespaces {
		if ns.Type == nsType {
			return ns
		}
	}
	return nil
}

func (d *DeleteAction) ManageAndUpdateResults(errorChan chan error, envTypes []environment.Type, healing Healing) error {
	err := d.commonNamespaceAction.ManageAndUpdateResults(errorChan, envTypes, healing)
	if err != nil {
		return err
	}
	namespaces, err := d.tenantRepo.GetNamespaces()
	if err != nil {
		return err
	}
	if d.deleteOptions.removeFromCluster {
		var names []string
		for _, ns := range namespaces {
			names = append(names, ns.Name)
		}
		if d.deleteOptions.keepTenant {
			if len(namespaces) != 0 {
				return fmt.Errorf("all namespaces of the tenant %s weren't properly removed - some namespaces %s still exist", namespaces[0].TenantID, names)
			}
		} else {
			if len(namespaces) == 0 {
				return d.tenantRepo.DeleteTenant()
			}
			return fmt.Errorf("cannot remove tenant %s from DB - some namespaces %s still exist", namespaces[0].TenantID, names)
		}
	}
	return nil
}

func (d *DeleteAction) HealingStrategy() HealingFuncGenerator {
	return d.redoStrategy(func(openShiftService *ServiceBuilder, nsTypes []environment.Type, existingNamespaces []*tenant.Namespace) error {
		return openShiftService.Delete(nsTypes, existingNamespaces, d.deleteOptions.DisableSelfHealing())
	})
}

func NewUpdateAction(tenantRepo tenant.Repository, requestCtx context.Context, existingNamespaces []*tenant.Namespace, actionOpts *ActionOptions) *UpdateAction {
	return &UpdateAction{
		withExistingNamespacesAction: &withExistingNamespacesAction{
			commonNamespaceAction: &commonNamespaceAction{
				method:        http.MethodPatch,
				tenantRepo:    tenantRepo,
				actionOptions: actionOpts,
				requestCtx:    requestCtx,
				filterFunc:    isNotOfKind(environment.ValKindProjectRequest),
			},
			existingNamespaces: existingNamespaces,
		},
	}
}

type UpdateAction struct {
	*withExistingNamespacesAction
}

func (u *UpdateAction) GetNamespaceEntity(nsTypeService EnvironmentTypeService) (*tenant.Namespace, error) {
	return u.getNamespaceFor(nsTypeService.GetType()), nil
}

func (u *UpdateAction) UpdateNamespace(env *environment.EnvData, cluster *cluster.Cluster, namespace *tenant.Namespace, failed bool) {
	state := tenant.Failed
	if !failed {
		state = tenant.Ready
		namespace.Version = env.Version()
	}
	namespace.UpdateData(env, cluster, state)
	err := u.tenantRepo.SaveNamespace(namespace)
	if err != nil {
		sentry.LogError(u.requestCtx, map[string]interface{}{
			"env_type": env.EnvType,
			"cluster":  cluster.APIURL,
			"tenant":   namespace.TenantID,
			"state":    state,
		}, err, "updating namespace entity failed")
	}
}

func (u *UpdateAction) GetOperationSets(envService EnvironmentTypeService, client Client) (*environment.EnvData, []OperationSet, error) {
	envAndObjectsManager, sets, err := u.getOperationSets(envService, client)
	if err != nil {
		return envAndObjectsManager.EnvData, sets, err
	}
	previousVersion := u.getNamespaceFor(envService.GetType()).Version
	objectsToDelete, err := envAndObjectsManager.GetMissingObjectsComparingWith(previousVersion)
	if err != nil {
		sentry.LogError(u.requestCtx, map[string]interface{}{
			"env_type":         envService.GetType(),
			"cluster":          client.MasterURL,
			"namespace-name":   envService.GetNamespaceName(),
			"previous-version": previousVersion,
		}, err, "unable to retrieve objects that should be removed from the namespace")
		return envAndObjectsManager.EnvData, sets, nil
	}
	if len(objectsToDelete) > 0 {
		for index, set := range sets {
			if set.Method == http.MethodDelete {
				sets[index].Objects = append(sets[index].Objects, objectsToDelete...)
				sort.Sort(sort.Reverse(environment.ByKind(sets[index].Objects)))
				return envAndObjectsManager.EnvData, sets, nil
			}
		}
		deleteSet := NewOperationSet(http.MethodDelete, objectsToDelete)
		sort.Sort(sort.Reverse(environment.ByKind(deleteSet.Objects)))
		sets = append(sets, deleteSet)
	}
	return envAndObjectsManager.EnvData, sets, nil
}

func (u *UpdateAction) HealingStrategy() HealingFuncGenerator {
	return u.redoStrategy(func(openShiftService *ServiceBuilder, nsTypes []environment.Type, existingNamespaces []*tenant.Namespace) error {
		return openShiftService.Update(nsTypes, existingNamespaces, u.actionOptions.DisableSelfHealing())
	})
}

func (w *withExistingNamespacesAction) redoStrategy(
	toRedo func(openShiftService *ServiceBuilder, nsTypes []environment.Type, existingNamespaces []*tenant.Namespace) error) HealingFuncGenerator {

	return func(openShiftService *ServiceBuilder) Healing {
		return func(originalError error) error {
			errMsgSuffix := fmt.Sprintf("while doing self-healing operations triggered by error: [%s]", originalError)
			namespaces, err := w.tenantRepo.GetNamespaces()
			if err != nil {
				return errors.Wrapf(err, "unable to get namespaces %s", errMsgSuffix)
			}
			err = toRedo(openShiftService, environment.DefaultEnvTypes, namespaces)
			if err != nil {
				return errors.Wrapf(err, "unable to redo the given action for the existing namespaces %s", errMsgSuffix)
			}
			return nil
		}
	}
}

type FilterFunc func(environment.Object) bool

func isOfKind(kinds ...string) FilterFunc {
	return func(vs environment.Object) bool {
		kind := environment.GetKind(vs)
		for _, k := range kinds {
			if k == kind {
				return true
			}
		}
		return false
	}
}

func isNotOfKind(kinds ...string) FilterFunc {
	f := isOfKind(kinds...)
	return func(vs environment.Object) bool {
		return !f(vs)
	}
}
