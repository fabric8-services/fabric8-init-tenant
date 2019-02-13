package openshift_test

import (
	"fmt"
	"github.com/fabric8-services/fabric8-tenant/environment"
	"github.com/fabric8-services/fabric8-tenant/openshift"
	"github.com/fabric8-services/fabric8-tenant/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
	"gopkg.in/yaml.v2"
	"net/http"
	"net/url"
	"testing"
)

var boundPVC = `{"kind":"PersistentVolumeClaim","apiVersion":"v1","metadata":{"name":"jenkins-home","namespace":"john-jenkins",
"selfLink":"/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home","uid":"e7c571fa-1598-11e9-aef5-525400d75155",
"resourceVersion":"360049","creationTimestamp":"2019-01-11T12:03:27Z","labels":{"app":"jenkins","provider":"fabric8","version":"123abc",
"version-quotas":"123abc"},"annotations":{"kubectl.kubernetes.io/last-applied-configuration":"{\"apiVersion\":\"v1\",\"kind\":\"PersistentVolumeClaim\",
\"metadata\":{\"annotations\":{},\"labels\":{\"app\":\"jenkins\",\"provider\":\"fabric8\",\"version\":\"123abc\",\"version-quotas\":\"123abc\"},
\"name\":\"jenkins-home\",\"namespace\":\"john-jenkins\"},\"spec\":{\"accessModes\":[\"ReadWriteOnce\"],
\"resources\":{\"requests\":{\"storage\":\"1Gi\"}}}}\n","pv.kubernetes.io/bind-completed":"yes","pv.kubernetes.io/bound-by-controller":"yes"}},
"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}},"volumeName":"pv0052"},"status":{"phase":"Bound",
"accessModes":["ReadWriteOnce","ReadWriteMany","ReadOnlyMany"],"capacity":{"storage":"100Gi"}}}`

var terminatingPVC = `{"kind":"PersistentVolumeClaim","apiVersion":"v1","metadata":{"name":"jenkins-home","namespace":"john-jenkins",
"selfLink":"/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home","uid":"e7c571fa-1598-11e9-aef5-525400d75155",
"resourceVersion":"360049","creationTimestamp":"2019-01-11T12:03:27Z","labels":{"app":"jenkins","provider":"fabric8","version":"123abc",
"version-quotas":"123abc"},"annotations":{"kubectl.kubernetes.io/last-applied-configuration":"{\"apiVersion\":\"v1\",\"kind\":\"PersistentVolumeClaim\",
\"metadata\":{\"annotations\":{},\"labels\":{\"app\":\"jenkins\",\"provider\":\"fabric8\",\"version\":\"123abc\",\"version-quotas\":\"123abc\"},
\"name\":\"jenkins-home\",\"namespace\":\"john-jenkins\"},\"spec\":{\"accessModes\":[\"ReadWriteOnce\"],
\"resources\":{\"requests\":{\"storage\":\"1Gi\"}}}}\n","pv.kubernetes.io/bind-completed":"yes","pv.kubernetes.io/bound-by-controller":"yes"}},
"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}},"volumeName":"pv0052"},"status":{"phase":"Terminating",
"accessModes":["ReadWriteOnce","ReadWriteMany","ReadOnlyMany"],"capacity":{"storage":"100Gi"}}}`

var podRunning = `{"kind":"Table","apiVersion":"meta.k8s.io/v1beta1","metadata":{"selfLink":"/api/v1/namespaces/mjobanek-preview4-jenkins/pods/jenkins-1-deploy",
"resourceVersion":"586168153"},"rows":[{"cells":["jenkins-1-deploy","1/1","Running",0,"41s","10.130.59.41","ip-172-31-76-248.us-east-2.compute.internal","\u003cnone\u003e"],
"object":{"kind":"PartialObjectMetadata","apiVersion":"meta.k8s.io/v1beta1","metadata":{"name":"jenkins-1-deploy","namespace":"mjobanek-preview4-jenkins",
"selfLink":"/api/v1/namespaces/mjobanek-preview4-jenkins/pods/jenkins-1-deploy","uid":"60431091-2e11-11e9-ae24-02074d91bc8a","resourceVersion":"586168153",
"creationTimestamp":"2019-02-11T15:26:17Z","labels":{"openshift.io/deployer-pod-for.name":"jenkins-1"},"annotations":{"kubernetes.io/limit-ranger":"LimitRanger plugin set: 
cpu, memory request for container deployment; cpu, memory limit for container deployment","openshift.io/deployment-config.name":"jenkins","openshift.io/deployment.name":
"jenkins-1","openshift.io/scc":"restricted"},"ownerReferences":[{"apiVersion":"v1","kind":"ReplicationController","name":"jenkins-1","uid":"5ff61988-2e11-11e9-ae24-02074d91bc8a"}]}}}]}`

var pvcToSet = `
- apiVersion: v1
  kind: PersistentVolumeClaim
  metadata:
    labels:
      app: jenkins
      provider: fabric8
      version: 123
      version-quotas: 456
    name: jenkins-home
    namespace: john-jenkins
  spec:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 1Gi
    userrestriction:
      users:
      - master-user
      - john@ibm-redhat.com
`

var projectRequestJenkins = `- apiVersion: v1
  kind: ProjectRequest
  metadata:
    annotations:
      openshift.io/description: john Jenkins Environment
      openshift.io/display-name: john Jenkins
      openshift.io/requester: john
    labels:
      app: fabric8-tenant-jenkins
      provider: fabric8
      version: 123
      version-quotas: john
    name: john-jenkins
`

var projectRequestUser = `- apiVersion: v1
  kind: ProjectRequest
  metadata:
    annotations:
      openshift.io/description: john Environment
      openshift.io/display-name: john
      openshift.io/requester: john
    labels:
      app: fabric8-tenant
      provider: fabric8
      version: 123
      version-quotas: john
    name: john
`

var tokenProducer = func(forceMasterToken bool) string {
	if forceMasterToken {
		return "master-token"
	}
	return "user-token"
}

func TestGetExistingObjectAndMerge(t *testing.T) {
	// given
	defer gock.OffAll()
	callbackContext := newCallbackContext(t, "PATCH", environment.ValKindPersistentVolumeClaim, pvcToSet)

	gock.New("https://starter.com").
		Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
		Reply(200).
		BodyString(boundPVC)

	// when
	methodDef, body, err := openshift.GetObjectAndMerge.Create(newBeforeCallbackFunc(callbackContext))(callbackContext)

	// then
	assert.NoError(t, err)
	assert.Equal(t, callbackContext.Method, methodDef)
	var actualObject environment.Object
	assert.NoError(t, yaml.Unmarshal(body, &actualObject))
	assert.Equal(t, callbackContext.Object, actualObject)
	assert.Equal(t, openshift.GetObjectAndMergeName, openshift.GetObjectAndMerge.Name)
}

func TestGetExistingObjectAndWaitTillIsNotTerminating(t *testing.T) {
	// given
	defer gock.OffAll()
	callbackContext := newCallbackContext(t, "PATCH", environment.ValKindPersistentVolumeClaim, pvcToSet)

	terminatingCalls := 0
	gock.New("https://starter.com").
		Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
		SetMatcher(test.SpyOnCalls(&terminatingCalls)).
		Reply(200).
		BodyString(terminatingPVC)
	boundCalls := 0
	gock.New("https://starter.com").
		Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
		SetMatcher(test.SpyOnCalls(&boundCalls)).
		Reply(200).
		BodyString(boundPVC)

	// when
	methodDef, body, err := openshift.GetObjectAndMerge.Create(newBeforeCallbackFunc(callbackContext))(callbackContext)

	// then
	assert.NoError(t, err)
	assert.Equal(t, callbackContext.Method, methodDef)
	var actualObject environment.Object
	assert.NoError(t, yaml.Unmarshal(body, &actualObject))
	assert.Equal(t, callbackContext.Object, actualObject)
	assert.Equal(t, openshift.GetObjectAndMergeName, openshift.GetObjectAndMerge.Name)
	assert.Equal(t, 1, terminatingCalls)
	assert.Equal(t, 1, boundCalls)
}

func TestGetMissingObjectAndMerge(t *testing.T) {
	// given
	defer gock.OffAll()
	callbackContext := newCallbackContext(t, "PATCH", environment.ValKindPersistentVolumeClaim, pvcToSet)

	gock.New("https://starter.com").
		Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
		Reply(404)

	// when
	methodDef, body, err := openshift.GetObjectAndMerge.Create(newBeforeCallbackFunc(callbackContext))(callbackContext)

	// then
	assert.NoError(t, err)
	postMethodDef, err := callbackContext.ObjEndpoints.GetMethodDefinition("POST", callbackContext.Object)
	assert.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%+v", *postMethodDef), fmt.Sprintf("%+v", *methodDef))
	var actualObject environment.Object
	assert.NoError(t, yaml.Unmarshal(body, &actualObject))
	assert.Equal(t, callbackContext.Object, actualObject)
}

func TestWhenNoConflictThenJustCheckResponseCode(t *testing.T) {
	// given
	callbackContext := newCallbackContext(t, "POST", environment.ValKindPersistentVolumeClaim, pvcToSet)

	t.Run("original response is 200 and error is nil, so no error is returned", func(t *testing.T) {
		// given
		defer gock.OffAll()
		result := openshift.NewResult(&http.Response{StatusCode: http.StatusOK}, []byte{}, nil)

		// when
		callbackResult, err := openshift.WhenConflictThenDeleteAndRedo.Create(newAfterCallbackFunc(result, nil))(callbackContext)

		// then
		assert.NoError(t, err)
		assert.Equal(t, result, callbackResult)
	})

	t.Run("original response is 404 and error is nil, so an error is returned", func(t *testing.T) {
		// given
		defer gock.OffAll()
		url, err := url.Parse("https://starter.com/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home")
		require.NoError(t, err)
		result := openshift.NewResult(&http.Response{
			StatusCode: http.StatusNotFound,
			Request: &http.Request{
				Method: http.MethodPost,
				URL:    url,
			},
		}, []byte{}, nil)

		// when
		callbackResult, err := openshift.WhenConflictThenDeleteAndRedo.Create(newAfterCallbackFunc(result, nil))(callbackContext)

		// then
		assert.NoError(t, err)
		test.AssertError(t, openshift.CheckHTTPCode(callbackResult, err),
			test.HasMessageContaining("server responded with status: 404 for the POST request"))
		assert.Equal(t, result, callbackResult)
	})

	t.Run("original response nil and error is not nil, so the same error is returned", func(t *testing.T) {
		// given
		defer gock.OffAll()
		expErr := fmt.Errorf("unexpected format")
		result := openshift.NewResult(nil, []byte{}, nil)

		// when
		callbackResult, err := openshift.WhenConflictThenDeleteAndRedo.Create(newAfterCallbackFunc(result, expErr))(callbackContext)
		assert.Equal(t, result, callbackResult)

		// then
		assert.Equal(t, expErr, err)
	})
	assert.Equal(t, openshift.WhenConflictThenDeleteAndRedoName, openshift.WhenConflictThenDeleteAndRedo.Name)
}

func TestWhenConflictThenDeleteAndRedoAction(t *testing.T) {
	// given
	callbackContext := newCallbackContext(t, "POST", environment.ValKindPersistentVolumeClaim, pvcToSet)

	t.Run("both delete and redo post is successful", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New("https://starter.com").
			Delete("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Reply(200)
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Reply(404)
		gock.New("https://starter.com").
			Post("/api/v1/namespaces/john-jenkins/persistentvolumeclaims").
			SetMatcher(test.ExpectRequest(test.HasBodyContainingObject(callbackContext.Object))).
			Reply(200)
		result := openshift.NewResult(&http.Response{StatusCode: http.StatusConflict}, []byte{}, nil)

		// when
		callbackResult, err := openshift.WhenConflictThenDeleteAndRedo.Create(newAfterCallbackFunc(result, nil))(callbackContext)

		// then
		assert.NoError(t, err)
		assert.Equal(t, 200, callbackResult.Response.StatusCode)
	})

	t.Run("when delete fails, then it returns an error", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New("https://starter.com").
			Delete("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Reply(500)
		result := openshift.NewResult(&http.Response{StatusCode: http.StatusConflict}, []byte{}, nil)

		// when
		callbackResult, err := openshift.WhenConflictThenDeleteAndRedo.Create(newAfterCallbackFunc(result, nil))(callbackContext)

		// then
		test.AssertError(t, err,
			test.HasMessageContaining("delete request failed while removing an object because of a conflict"),
			test.HasMessageContaining("server responded with status: 500 for the DELETE request"))
		assert.Equal(t, result, callbackResult)
	})

	t.Run("when there is a second conflict while redoing the action, then it return an error and stops redoing", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New("https://starter.com").
			Delete("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Reply(200)
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Reply(404)
		gock.New("https://starter.com").
			Post("/api/v1/namespaces/john-jenkins/persistentvolumeclaims").
			SetMatcher(test.ExpectRequest(test.HasBodyContainingObject(callbackContext.Object))).
			Reply(409)
		result := openshift.NewResult(&http.Response{StatusCode: http.StatusConflict}, []byte{}, nil)

		// when
		callbackResult, err := openshift.WhenConflictThenDeleteAndRedo.Create(newAfterCallbackFunc(result, nil))(callbackContext)

		// then
		test.AssertError(t, err,
			test.HasMessageContaining("redoing an action POST failed after the object was successfully removed because of a previous conflict"),
			test.HasMessageContaining("server responded with status: 409 for the POST request"))
		assert.Equal(t, 409, callbackResult.Response.StatusCode)
		assert.NotEqual(t, result, callbackResult)
	})
}

func TestIgnoreWhenDoesNotExist(t *testing.T) {
	// given
	callbackContext := newCallbackContext(t, "DELETE", environment.ValKindPersistentVolumeClaim, pvcToSet)

	t.Run("when there is 404, then it ignores it even if there is an error", func(t *testing.T) {
		// given
		result := openshift.NewResult(&http.Response{StatusCode: http.StatusNotFound}, []byte{}, fmt.Errorf("not found"))

		// when
		callbackResult, err := openshift.IgnoreWhenDoesNotExistOrConflicts.Create(newAfterCallbackFunc(result, fmt.Errorf("not found")))(callbackContext)

		// then
		assert.NoError(t, err)
		assert.Empty(t, callbackResult)
	})

	t.Run("when there is 409, then it ignores it even if there is an error", func(t *testing.T) {
		// given
		result := openshift.NewResult(&http.Response{StatusCode: http.StatusConflict}, []byte{}, fmt.Errorf("conflict"))

		// when
		callbackResult, err := openshift.IgnoreWhenDoesNotExistOrConflicts.Create(newAfterCallbackFunc(result, fmt.Errorf("conflict")))(callbackContext)

		// then
		assert.NoError(t, err)
		assert.Empty(t, callbackResult)
	})

	t.Run("when code is 200 but an error is not nil, then it returns the error", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New("https://starter.com").Times(0)
		result := openshift.NewResult(&http.Response{StatusCode: http.StatusOK}, []byte{}, nil)

		// when
		callbackResult, err := openshift.IgnoreWhenDoesNotExistOrConflicts.Create(newAfterCallbackFunc(result, fmt.Errorf("wrong request")))(callbackContext)

		// then
		test.AssertError(t, err, test.HasMessage("wrong request"))
		assert.Equal(t, result, callbackResult)
	})

	t.Run("when there status code is 500, then it returns the same result", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New("https://starter.com").Times(0)
		url, err := url.Parse("https://starter.com/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home")
		require.NoError(t, err)
		result := openshift.NewResult(&http.Response{
			StatusCode: http.StatusInternalServerError,
			Request: &http.Request{
				Method: http.MethodDelete,
				URL:    url,
			},
		}, []byte{}, nil)

		// when
		callbackResult, err := openshift.IgnoreWhenDoesNotExistOrConflicts.Create(newAfterCallbackFunc(result, nil))(callbackContext)

		// then
		assert.NoError(t, err)
		test.AssertError(t, openshift.CheckHTTPCode(callbackResult, err),
			test.HasMessageContaining("server responded with status: 500 for the DELETE request"))
		assert.Equal(t, result, callbackResult)
	})

	t.Run("when the status code is 200 and no error then it returns no error", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New("https://starter.com").Times(0)
		result := openshift.NewResult(&http.Response{StatusCode: http.StatusOK}, []byte{}, nil)

		// when
		callbackResult, err := openshift.IgnoreWhenDoesNotExistOrConflicts.Create(newAfterCallbackFunc(result, nil))(callbackContext)

		// then
		assert.NoError(t, err)
		assert.Equal(t, result, callbackResult)
	})

	assert.Equal(t, openshift.IgnoreWhenDoesNotExistName, openshift.IgnoreWhenDoesNotExistOrConflicts.Name)
}

//
func TestGetObject(t *testing.T) {
	// given
	callbackContext := newCallbackContext(t, "POST", environment.ValKindPersistentVolumeClaim, pvcToSet)

	t.Run("when returns 200, then it reads the object an checks status. everything is good, then return no error", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Reply(200).
			BodyString(`{"kind": "RoleBindingRestriction", "status": {"phase":"Active"}}`)
		result := openshift.NewResult(&http.Response{StatusCode: http.StatusOK}, []byte{}, nil)

		// when
		callbackResult, err := openshift.GetObject.Create(newAfterCallbackFunc(result, nil))(callbackContext)

		// then
		assert.NoError(t, err)
		assert.Equal(t, result, callbackResult)
	})

	t.Run("when returns 200, then it reads the object an checks status. when is missing then retries until is present", func(t *testing.T) {
		// given
		defer gock.OffAll()
		counter := 0
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Times(3).
			SetMatcher(test.SpyOnCalls(&counter)).
			Reply(200).
			BodyString(`{"kind": "RoleBindingRestriction"`)
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Reply(200).
			BodyString(`{"kind": "RoleBindingRestriction", "status": {"phase":"Active"}}`)
		result := openshift.NewResult(&http.Response{StatusCode: http.StatusOK}, []byte{}, nil)

		// when
		callbackResult, err := openshift.GetObject.Create(newAfterCallbackFunc(result, nil))(callbackContext)

		// then
		assert.NoError(t, err)
		assert.Equal(t, 3, counter)
		assert.Equal(t, result, callbackResult)
	})

	t.Run("when returns 200, but with invalid Body. then retries until everything is fine", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Reply(200).
			BodyString(`{"kind": "RoleBindingRestriction""`)
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Reply(200).
			BodyString(`{"kind": "RoleBindingRestriction", "status": {"phase":"Active"}}`)
		result := openshift.NewResult(&http.Response{StatusCode: http.StatusOK}, []byte{}, nil)

		// when
		callbackResult, err := openshift.GetObject.Create(newAfterCallbackFunc(result, nil))(callbackContext)

		// then
		assert.NoError(t, err)
		assert.Equal(t, result, callbackResult)
	})

	t.Run("when returns 404, then retries until everything is fine", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Reply(404)
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Reply(200).
			BodyString(`{"kind": "RoleBindingRestriction", "status": {"phase":"Active"}}`)
		result := openshift.NewResult(&http.Response{StatusCode: http.StatusOK}, []byte{}, nil)

		// when
		callbackResult, err := openshift.GetObject.Create(newAfterCallbackFunc(result, nil))(callbackContext)

		// then
		assert.NoError(t, err)
		assert.Equal(t, result, callbackResult)
	})

	t.Run("when always returns 404 then after 50 attempts it returns error", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Times(50).
			Reply(404)
		result := openshift.NewResult(&http.Response{StatusCode: http.StatusOK}, []byte{}, nil)

		// when
		callbackResult, err := openshift.GetObject.Create(newAfterCallbackFunc(result, nil))(callbackContext)

		// then
		test.AssertError(t, err, test.HasMessageContaining("unable to finish the action POST on a object"),
			test.HasMessageContaining("as there were 50 of unsuccessful retries to get the created objects from the cluster https://starter.com"))
		assert.Equal(t, result, callbackResult)
	})

	t.Run("when the status code is 404, then it returns the appropriate error", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New("https://starter.com").Times(0)
		url, err := url.Parse("https://starter.com/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home")
		require.NoError(t, err)
		result := openshift.NewResult(&http.Response{
			StatusCode: http.StatusNotFound,
			Request: &http.Request{
				Method: http.MethodPost,
				URL:    url,
			},
		}, []byte{}, nil)

		// when
		callbackResult, err := openshift.GetObject.Create(newAfterCallbackFunc(result, nil))(callbackContext)

		// then
		test.AssertError(t, err, test.HasMessageContaining("server responded with status: 404 for the POST request"))
		assert.Equal(t, result, callbackResult)
	})

	t.Run("when there is an error in the result, then it's returned", func(t *testing.T) {
		// given
		defer gock.OffAll()
		result := openshift.NewResult(&http.Response{StatusCode: http.StatusOK}, []byte{}, nil)

		// when
		callbackResult, err := openshift.GetObject.Create(newAfterCallbackFunc(result, fmt.Errorf("error")))(callbackContext)

		// then
		test.AssertError(t, err, test.HasMessage("error"))
		assert.Equal(t, result, callbackResult)
	})

	assert.Equal(t, openshift.GetObjectName, openshift.GetObject.Name)
}

func TestFailIfAlreadyExists(t *testing.T) {
	// given
	callbackContext := newCallbackContext(t, "POST", environment.ValKindProjectRequest, projectRequestJenkins)

	t.Run("when returns 200, then it returns error", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New("https://starter.com").
			Get("/oapi/v1/projects/john-jenkins").
			SetMatcher(test.ExpectRequest(test.HasBearerWithSub("master-token"))).
			Reply(200).
			BodyString(``)

		// when
		methodDef, body, err := openshift.FailIfAlreadyExists.Create(newBeforeCallbackFunc(callbackContext))(callbackContext)

		// then
		test.AssertError(t, err, test.HasMessageContaining("already exists"))
		assert.Nil(t, methodDef)
		assert.Nil(t, body)
	})

	t.Run("when returns 404, then it should return original method and body", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New("https://starter.com").
			Get("/oapi/v1/projects/john-jenkins").
			SetMatcher(test.ExpectRequest(test.HasBearerWithSub("master-token"))).
			Reply(404).
			BodyString(``)

		// when
		actualMethodDef, body, err := openshift.FailIfAlreadyExists.Create(newBeforeCallbackFunc(callbackContext))(callbackContext)

		// then
		require.NoError(t, err)
		assert.Equal(t, callbackContext.Method, actualMethodDef)
		assert.Contains(t, string(body), "name: john-jenkins")
	})

	t.Run("when returns 403, then it should return original method and body", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New("https://starter.com").
			Get("/oapi/v1/projects/john-jenkins").
			SetMatcher(test.ExpectRequest(test.HasBearerWithSub("master-token"))).
			Reply(403).
			BodyString(``)

		// when
		actualMethodDef, body, err := openshift.FailIfAlreadyExists.Create(newBeforeCallbackFunc(callbackContext))(callbackContext)

		// then
		require.NoError(t, err)
		assert.Equal(t, callbackContext.Method, actualMethodDef)
		assert.Contains(t, string(body), "name: john-jenkins")
	})
}

func TestFailIfAlreadyExistsForUserNamespaceShouldUseMasterToken(t *testing.T) {
	// given
	callbackContext := newCallbackContext(t, "POST", environment.ValKindProjectRequest, projectRequestUser)

	t.Run("when returns 200, then it returns error", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New("https://starter.com").
			Get("/oapi/v1/projects/john").
			SetMatcher(test.ExpectRequest(test.HasBearerWithSub("master-token"))).
			Reply(200).
			BodyString(``)

		// when
		methodDef, body, err := openshift.FailIfAlreadyExists.Create(newBeforeCallbackFunc(callbackContext))(callbackContext)

		// then
		test.AssertError(t, err, test.HasMessageContaining("already exists"))
		assert.Nil(t, methodDef)
		assert.Nil(t, body)
	})

	t.Run("when returns 404, then it should return original method and body", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New("https://starter.com").
			Get("/oapi/v1/projects/john").
			SetMatcher(test.ExpectRequest(test.HasBearerWithSub("master-token"))).
			Reply(404).
			BodyString(``)

		// when
		actualMethodDef, body, err := openshift.FailIfAlreadyExists.Create(newBeforeCallbackFunc(callbackContext))(callbackContext)

		// then
		require.NoError(t, err)
		assert.Equal(t, callbackContext.Method, actualMethodDef)
		assert.Contains(t, string(body), "name: john")
	})
}

func TestWaitUntilIsGone(t *testing.T) {
	// given
	callbackContext := newCallbackContext(t, "DELETE", environment.ValKindPersistentVolumeClaim, pvcToSet)
	result := openshift.NewResult(&http.Response{StatusCode: http.StatusOK}, []byte{}, nil)

	t.Run("wait until is in terminating state", func(t *testing.T) {
		defer gock.OffAll()
		terminatingCalls := 0
		boundCalls := 0
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			SetMatcher(test.SpyOnCalls(&boundCalls)).
			Times(2).
			Reply(200).
			BodyString(boundPVC)
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			SetMatcher(test.SpyOnCalls(&terminatingCalls)).
			Reply(200).
			BodyString(terminatingPVC)

		// when
		callbackResult, err := openshift.TryToWaitUntilIsGone.Create(newAfterCallbackFunc(result, nil))(callbackContext)

		// then
		assert.NoError(t, err)
		assert.Equal(t, openshift.TryToWaitUntilIsGoneName, openshift.TryToWaitUntilIsGone.Name)
		assert.Equal(t, 1, terminatingCalls)
		assert.Equal(t, 2, boundCalls)
		assert.Equal(t, result, callbackResult)
	})

	t.Run("wait until it returns 404", func(t *testing.T) {
		defer gock.OffAll()
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Times(2).
			Reply(200).
			BodyString(boundPVC)
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Reply(404)

		// when
		callbackResult, err := openshift.TryToWaitUntilIsGone.Create(newAfterCallbackFunc(result, nil))(callbackContext)

		// then
		assert.NoError(t, err)
		assert.Equal(t, openshift.TryToWaitUntilIsGoneName, openshift.TryToWaitUntilIsGone.Name)
		assert.Equal(t, result, callbackResult)
	})

	t.Run("wait until it returns 403", func(t *testing.T) {
		defer gock.OffAll()
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Reply(200).
			BodyString(boundPVC)
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home").
			Reply(403)

		// when
		callbackResult, err := openshift.TryToWaitUntilIsGone.Create(newAfterCallbackFunc(result, nil))(callbackContext)

		// then
		assert.NoError(t, err)
		assert.Equal(t, result, callbackResult)
	})

	t.Run("if gets result with 500, then returns an error", func(t *testing.T) {
		// given
		url, err := url.Parse("https://starter.com/api/v1/namespaces/john-jenkins/persistentvolumeclaims/jenkins-home")
		require.NoError(t, err)
		failingResult := openshift.NewResult(&http.Response{
			StatusCode: http.StatusInternalServerError,
			Request: &http.Request{
				Method: http.MethodPost,
				URL:    url,
			},
		}, []byte{}, nil)

		// when
		callbackResult, err := openshift.TryToWaitUntilIsGone.Create(newAfterCallbackFunc(failingResult, nil))(callbackContext)

		// then
		test.AssertError(t, err,
			test.HasMessageContaining("server responded with status: 500 for the POST request"))
		assert.Equal(t, failingResult, callbackResult)
	})
}

func TestWaitUntilIsRemoved(t *testing.T) {
	// given
	podObj := openshift.NewObject(environment.ValKindPod, "john-jenkins", "jenkins-1-deploy")
	obj, err := yaml.Marshal(environment.Objects{podObj})
	require.NoError(t, err)
	callbackContext := newCallbackContext(t, openshift.EnsureDeletion, environment.ValKindPod, string(obj))

	t.Run("wait until it returns 404 when is activated", func(t *testing.T) {
		defer gock.OffAll()
		notFoundCalls := 0
		gock.New(test.ClusterURL).
			Get("/api/v1/namespaces/john-jenkins/pods/jenkins-1-deploy").
			Times(2).
			Reply(200).
			BodyString(podRunning)
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/pods/jenkins-1-deploy").
			SetMatcher(test.SpyOnCalls(&notFoundCalls)).
			Reply(404)

		// when
		waitUntilIsRemoved := openshift.WaitUntilIsRemoved(true)
		method, body, err := waitUntilIsRemoved.Create(newBeforeCallbackFunc(callbackContext))(callbackContext)

		// then
		assert.NoError(t, err)
		assert.Equal(t, openshift.WaitUntilIsRemovedName, waitUntilIsRemoved.Name)
		assert.Nil(t, method)
		assert.Nil(t, body)
		assert.Equal(t, 1, notFoundCalls)
	})

	t.Run("wait until it returns 403 when is activated", func(t *testing.T) {
		defer gock.OffAll()
		gock.New(test.ClusterURL).
			Get("/api/v1/namespaces/john-jenkins/pods/jenkins-1-deploy").
			Times(2).
			Reply(200).
			BodyString(podRunning)
		gock.New("https://starter.com").
			Get("/api/v1/namespaces/john-jenkins/pods/jenkins-1-deploy").
			Reply(403)

		// when
		waitUntilIsRemoved := openshift.WaitUntilIsRemoved(true)
		method, body, err := waitUntilIsRemoved.Create(newBeforeCallbackFunc(callbackContext))(callbackContext)

		// then
		assert.NoError(t, err)
		assert.Nil(t, method)
		assert.Nil(t, body)
	})

	t.Run("don't do anything when is not activated", func(t *testing.T) {
		defer gock.OffAll()

		// when
		waitUntilIsRemoved := openshift.WaitUntilIsRemoved(false)
		method, body, err := waitUntilIsRemoved.Create(newBeforeCallbackFunc(callbackContext))(callbackContext)

		// then
		assert.NoError(t, err)
		assert.Equal(t, openshift.WaitUntilIsRemovedName, waitUntilIsRemoved.Name)
		assert.Nil(t, method)
		assert.Nil(t, body)
	})
}

func newCallbackContext(t *testing.T, method, kind, response string) openshift.CallbackContext {
	client := openshift.NewClient(nil, "https://starter.com", tokenProducer)
	var object environment.Objects
	require.NoError(t, yaml.Unmarshal([]byte(response), &object))
	bindingEndpoints := openshift.AllObjectEndpoints[kind]
	methodDefinition, err := bindingEndpoints.GetMethodDefinition(method, object[0])
	assert.NoError(t, err)
	return openshift.NewCallbackContext(client, object[0], bindingEndpoints, methodDefinition)
}

func newAfterCallbackFunc(result *openshift.Result, err error) openshift.AfterDoCallbackFunc {
	return func(context openshift.CallbackContext) (*openshift.Result, error) {
		return result, err
	}
}

func newBeforeCallbackFunc(callbackContext openshift.CallbackContext) openshift.BeforeDoCallbackFunc {
	return func(context openshift.CallbackContext) (*openshift.MethodDefinition, []byte, error) {
		return openshift.DefaultBeforeDoCallBack(callbackContext)
	}
}
