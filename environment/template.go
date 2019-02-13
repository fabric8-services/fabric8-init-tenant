package environment

import (
	"fmt"
	"regexp"
	"strings"

	authclient "github.com/fabric8-services/fabric8-tenant/auth/client"
	"github.com/fabric8-services/fabric8-tenant/configuration"
	"github.com/fabric8-services/fabric8-tenant/keycloak"
	"github.com/fabric8-services/fabric8-tenant/sentry"
	"gopkg.in/yaml.v2"
)

const (
	FieldKind          = "kind"
	FieldObjects       = "objects"
	FieldSpec          = "spec"
	FieldItems         = "items"
	FieldMetadata      = "metadata"
	FieldLabels        = "labels"
	FieldVersion       = "version"
	FieldVersionQuotas = "version-quotas"
	FieldNamespace     = "namespace"
	FieldName          = "name"
	FieldStatus        = "status"
	FieldParameters    = "parameters"

	ValKindTemplate                = "Template"
	ValKindNamespace               = "Namespace"
	ValKindConfigMap               = "ConfigMap"
	ValKindLimitRange              = "LimitRange"
	ValKindProject                 = "Project"
	ValKindProjectRequest          = "ProjectRequest"
	ValKindPersistentVolumeClaim   = "PersistentVolumeClaim"
	ValKindService                 = "Service"
	ValKindSecret                  = "Secret"
	ValKindServiceAccount          = "ServiceAccount"
	ValKindRoleBindingRestriction  = "RoleBindingRestriction"
	ValKindRoleBinding             = "RoleBinding"
	ValKindRole                    = "Role"
	ValKindRoute                   = "Route"
	ValKindJob                     = "Job"
	ValKindList                    = "List"
	ValKindDeployment              = "Deployment"
	ValKindDeploymentConfig        = "DeploymentConfig"
	ValKindResourceQuota           = "ResourceQuota"
	ValKindPod                     = "Pod"
	ValKindReplicationController   = "ReplicationController"
	ValKindDaemonSet               = "DaemonSet"
	ValKindReplicaSet              = "ReplicaSet"
	ValKindStatefulSet             = "StatefulSet"
	ValKindHorizontalPodAutoScaler = "HorizontalPodAutoScaler"
	ValKindCronJob                 = "CronJob"
	ValKindBuildConfig             = "BuildConfig"
	ValKindBuild                   = "Build"
	ValKindImageStream             = "ImageStream"

	varUserName              = "USER_NAME"
	varProjectUser           = "PROJECT_USER"
	varProjectRequestingUser = "PROJECT_REQUESTING_USER"
	varProjectAdminUser      = "PROJECT_ADMIN_USER"
	varKeycloakURL           = "KEYCLOAK_URL"
	varCommit                = "COMMIT"
	varCommitQuotas          = "COMMIT_QUOTAS"
	varDeployType            = "DEPLOY_TYPE"
	varKeycloakOsoEndpoint   = "KEYCLOAK_OSO_ENDPOINT"
	varKeycloakGHEndpoint    = "KEYCLOAK_GITHUB_ENDPOINT"
)

var sortOrder = map[string]int{
	ValKindNamespace:               1,
	ValKindProjectRequest:          1,
	ValKindRole:                    2,
	ValKindRoleBindingRestriction:  3,
	ValKindLimitRange:              4,
	ValKindResourceQuota:           5,
	ValKindSecret:                  6,
	ValKindServiceAccount:          7,
	ValKindService:                 8,
	ValKindRoleBinding:             9,
	ValKindPod:                     9,
	ValKindPersistentVolumeClaim:   10,
	ValKindReplicaSet:              10,
	ValKindConfigMap:               11,
	ValKindReplicationController:   11,
	ValKindDeploymentConfig:        12,
	ValKindDeployment:              12,
	ValKindRoute:                   13,
	ValKindJob:                     14,
	ValKindDaemonSet:               15,
	ValKindStatefulSet:             16,
	ValKindHorizontalPodAutoScaler: 17,
	ValKindCronJob:                 18,
	ValKindBuildConfig:             19,
	ValKindBuild:                   20,
	ValKindImageStream:             21,
}

type Objects []Object
type Object map[interface{}]interface{}

func (o Object) ToString() string {
	out, err := yaml.Marshal(o)
	if err != nil {
		sentry.LogError(nil, map[string]interface{}{"object": o}, err, "marshalling of the object failed")
		return fmt.Sprintf("%s", o)
	}
	return string(out)
}

type Template struct {
	Filename      string
	DefaultParams map[string]string
	Content       string
	Version       string
}

var (
	specialCharRegexp = regexp.MustCompile("[^a-z0-9]")
	variableRegexp    = regexp.MustCompile(`\${([A-Z_0-9]+)}`)
)

func newTemplate(filename string, defaultParams map[string]string, version string) Template {
	return Template{
		Filename:      filename,
		DefaultParams: defaultParams,
		Version:       version,
	}
}

func (t *Template) Process(vars map[string]string) (Objects, error) {
	var objects Objects
	templateVars := merge(vars, t.DefaultParams, false)
	paramsFromTemplate, err := t.getParamsFromTemplate()
	if err != nil {
		return objects, err
	}
	if paramsFromTemplate != nil {
		templateVars = merge(paramsFromTemplate, templateVars, false)
	}
	pt, err := t.ReplaceVars(templateVars)
	if err != nil {
		return objects, err
	}
	return ParseObjects(pt)
}

func (t *Template) getParamsFromTemplate() (map[string]string, error) {
	var template Object

	err := yaml.Unmarshal([]byte(t.Content), &template)
	if err != nil {
		return nil, err
	}
	if paramsPart, exist := template[FieldParameters]; exist {
		templateParams := make(map[string]string)
		if params, ok := paramsPart.([]interface{}); ok {
			for _, paramObj := range params {
				if param, ok := paramObj.(Object); ok {
					if name, exist := param["name"]; exist {
						if value, exist := param["value"]; exist {
							templateParams[fmt.Sprint(name)] = fmt.Sprint(value)
						}
					}
				}
			}
			return templateParams, nil
		}
	}
	return nil, nil
}

// Process takes a K8/Openshift Template as input and resolves the variable expresions
func (t *Template) ReplaceVars(variables map[string]string) (string, error) {
	return string(variableRegexp.ReplaceAllFunc([]byte(t.Content), func(found []byte) []byte {
		variableName := toVariableName(string(found))
		if variable, ok := variables[variableName]; ok {
			return []byte(variable)
		}
		return found
	})), nil
}

func CollectVars(osUsername, nsBaseName, masterUser string, config *configuration.Data) map[string]string {
	vars := map[string]string{
		varUserName:              nsBaseName,
		varProjectUser:           osUsername,
		varProjectRequestingUser: osUsername,
		varProjectAdminUser:      masterUser,
	}

	return merge(vars, getVariables(config), false)
}

// RetrieveUserName returns a safe namespace basename based on a username
func RetrieveUserName(openshiftUsername string) string {
	return specialCharRegexp.ReplaceAllString(strings.Split(openshiftUsername, "@")[0], "-")
}

func getVariables(config *configuration.Data) map[string]string {
	keycloakConfig := keycloak.Config{
		BaseURL: config.GetKeycloakURL(),
		Realm:   config.GetKeycloakRealm(),
		Broker:  config.GetKeycloakOpenshiftBroker(),
	}

	templateVars, err := config.GetTemplateValues()
	if err != nil {
		panic(err)
	}
	templateVars[varKeycloakURL] = ""
	templateVars[varKeycloakOsoEndpoint] = keycloakConfig.CustomBrokerTokenURL("openshift-v3")
	templateVars[varKeycloakGHEndpoint] = fmt.Sprintf("%s%s?for=https://github.com", config.GetAuthURL(), authclient.RetrieveTokenPath())

	return templateVars
}

func merge(target, second map[string]string, overwrite bool) map[string]string {
	if len(second) == 0 {
		return target
	}
	result := clone(second)
	for k, v := range target {
		if _, exist := result[k]; overwrite || !exist {
			result[k] = v
		}
	}
	return result
}

func clone(maps map[string]string) map[string]string {
	maps2 := make(map[string]string)
	for k2, v2 := range maps {
		maps2[k2] = v2
	}
	return maps2
}

func toVariableName(exp string) string {
	return exp[:len(exp)-1][2:]
}

// ParseObjects return a string yaml and return a array of the objects/items from a Template/List kind
func ParseObjects(source string) (Objects, error) {
	var template Object

	err := yaml.Unmarshal([]byte(source), &template)
	if err != nil {
		return nil, err
	}

	if GetKind(template) == ValKindTemplate || GetKind(template) == ValKindList {
		var ts []interface{}
		if GetKind(template) == ValKindTemplate {
			ts = template[FieldObjects].([]interface{})
		} else if GetKind(template) == ValKindList {
			ts = template[FieldItems].([]interface{})
		}
		var objs Objects
		for _, obj := range ts {
			parsedObj := obj.(Object)
			stringKeys := make(Object, len(parsedObj))
			for key, value := range parsedObj {
				stringKeys[key.(string)] = value
			}
			objs = append(objs, stringKeys)
		}
		return objs, nil
	}

	return Objects{template}, nil
}

func GetName(obj Object) string {
	if meta, metaFound := obj[FieldMetadata].(Object); metaFound {
		if name, nameFound := meta[FieldName].(string); nameFound {
			return name
		}
	}
	return ""
}

func GetNamespace(obj Object) string {
	if meta, metaFound := obj[FieldMetadata].(Object); metaFound {
		if name, nameFound := meta[FieldNamespace].(string); nameFound {
			return name
		}
	}
	return ""
}

func GetKind(obj Object) string {
	if kind, kindFound := obj[FieldKind].(string); kindFound {
		return kind
	}
	return ""
}

func HasValidStatus(obj Object) bool {
	return len(GetStatus(obj)) > 0
}

func GetStatus(obj Object) Object {
	if status, statusFound := obj[FieldStatus].(Object); statusFound {
		return status
	}
	return nil
}

func GetLabelVersion(obj Object) string {
	return GetLabel(obj, FieldVersion)
}

func GetLabel(obj Object, name string) string {
	if meta, metaFound := obj[FieldMetadata].(Object); metaFound {
		if labels, labelsFound := meta[FieldLabels].(Object); labelsFound {
			if label, labelFound := labels[name]; labelFound {
				return fmt.Sprint(label)
			}
		}
	}
	return ""
}

// ByKind represents a list of Openshift objects sortable by Kind
type ByKind Objects

func (a ByKind) Len() int      { return len(a) }
func (a ByKind) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByKind) Less(i, j int) bool {
	iO := 30
	jO := 30

	if val, ok := sortOrder[GetKind(a[i])]; ok {
		iO = val
	}
	if val, ok := sortOrder[GetKind(a[j])]; ok {
		jO = val
	}
	return iO < jO
}
