package openshift_test

import (
	"fmt"
	"testing"

	"github.com/fabric8-services/fabric8-tenant/openshift"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var applyTemplate = `
---
apiVersion: v1
kind: Template
metadata:
  labels:
    provider: fabric8
    project: fabric8-tenant-team-environments
    version: 1.0.58
    group: io.fabric8.online.packages
  name: fabric8-tenant-team-envi
objects:
- apiVersion: v1
  kind: Project
  metadata:
    annotations:
      openshift.io/description: Test-Project-Description
      openshift.io/display-name: Test-Project-Name
      openshift.io/requester: Aslak-User
    labels:
      provider: fabric8
      project: fabric8-tenant-team-environments
      version: 1.0.58
      group: io.fabric8.online.packages
    name: aslak-test
`

var parseNamespaceTemplate = `
---
apiVersion: v1
kind: Template
metadata:
  labels:
    provider: fabric8
    project: fabric8-tenant-team-environments
    version: 1.0.58
    group: io.fabric8.online.packages
  name: fabric8-tenant-team-envi
objects:
- apiVersion: v1
  kind: Namespace
  metadata:
    annotations:
      openshift.io/description: Test-Project-Description
      openshift.io/display-name: Test-Project-Name
      openshift.io/requester: Aslak-User
    labels:
      provider: fabric8
      project: fabric8-tenant-team-environments
      version: 1.0.58
      group: io.fabric8.online.packages
    name: aslak-test
`

var sortTemplate = `
---
apiVersion: v1
kind: Template
objects:
- apiVersion: v1
  kind: Secret
  metadata:
    name: aslak-test
- apiVersion: v1
  kind: ProjectRequest
  metadata:
    name: aslak-test
- apiVersion: v1
  kind: ServiceAccount
  metadata:
    name: aslak-test
- apiVersion: v1
  kind: RoleBinding
  metadata:
    name: aslak-test
- apiVersion: v1
  kind: RoleBindingRestriction
  metadata:
    name: aslak-test
- apiVersion: v1
  kind: ResourceQuota
  metadata:
    name: aslak-test
- apiVersion: v1
  kind: LimitRange
  metadata:
    name: aslak-test
`

// Ignore for now. Need vcr setup to record openshift interactions
func xTestApply(t *testing.T) {
	opts := openshift.ApplyOptions{
		Config: openshift.Config{
			MasterURL: "https://tsrv.devshift.net:8443",
			Token:     "HMs8laMmBSsJi8hpMDOtiglbXJ-2eyymE1X46ax5wX8",
		},
	}

	t.Run("apply single project", func(t *testing.T) {
		result := openshift.Apply(applyTemplate, opts)
		assert.NoError(t, result, "apply error")
	})

}

func xTestSort(t *testing.T) {
	l, err := openshift.ParseObjects(sortTemplate, "")
	require.NoError(t, err)

	assert.Equal(t, "ProjectRequest", kind(l[0]))
	assert.Equal(t, "RoleBindingRestriction", kind(l[1]))
	assert.Equal(t, "LimitRange", kind(l[2]))
	assert.Equal(t, "ResourceQuota", kind(l[3]))
}

func TestParseNamespace(t *testing.T) {
	l, err := openshift.ParseObjects(parseNamespaceTemplate, "")
	require.NoError(t, err)

	assert.Equal(t, "Namespace", kind(l[0]))
}

func kind(object map[interface{}]interface{}) string {
	return object["kind"].(string)
}

func TestA(t *testing.T) {
	opts := &openshift.ApplyOptions{Callback: A}
	fmt.Println(opts.Callback)
	opts2 := opts.WithNamespace("a")
	fmt.Println(opts2.Callback)
}

func A(statusCode int, method string, request, response map[interface{}]interface{}) (string, map[interface{}]interface{}) {
	fmt.Println("A")
	return "", nil
}
