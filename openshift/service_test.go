package openshift_test

import (
	"github.com/fabric8-services/fabric8-common/convert/ptr"
	"github.com/fabric8-services/fabric8-tenant/environment"
	"github.com/fabric8-services/fabric8-tenant/openshift"
	"github.com/fabric8-services/fabric8-tenant/tenant"
	"github.com/fabric8-services/fabric8-tenant/test"
	"github.com/fabric8-services/fabric8-tenant/test/assertion"
	"github.com/fabric8-services/fabric8-tenant/test/doubles"
	"github.com/fabric8-services/fabric8-tenant/test/gormsupport"
	tf "github.com/fabric8-services/fabric8-tenant/test/testfixture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"gopkg.in/h2non/gock.v1"
	"testing"
)

var templateHeader = `
---
apiVersion: v1
kind: Template
metadata:
  labels:
    provider: fabric8
    project: fabric8-tenant-team-environments
    version: 1.0.58
    group: io.fabric8.online.packages
  name: fabric8-tenant-team
objects:
`
var projectRequestObject = `
- apiVersion: v1
  kind: ProjectRequest
  metadata:
    annotations:
      openshift.io/description: user-Project-Description
      openshift.io/display-name: user-Project-Name
      openshift.io/requester: Aslak-User
    labels:
      provider: fabric8
      project: fabric8-tenant-team-environments
      version: 1.0.58
      group: io.fabric8.online.packages
    name: ${USER_NAME}
`
var roleBindingObject = `
- apiVersion: v1
  kind: RoleBinding
  metadata:
    labels:
      app: fabric8-tenant-user
      provider: fabric8
      version: 1.0.58
    name: dsaas-admin
    namespace: ${USER_NAME}
  roleRef:
    name: admin
  subjects:
  - kind: User
    name: ${PROJECT_ADMIN_USER}
  userNames:
  - ${PROJECT_ADMIN_USER}
`

var roleBindingRestrictionRun = `
- apiVersion: v1
  kind: RoleBindingRestriction
  metadata:
    labels:
      app: fabric8-tenant-che-mt
      provider: fabric8
      version: 2.0.85
      group: io.fabric8.tenant.packages
    name: dsaas-user-access
    namespace: ${USER_NAME}
  spec:
    userrestriction:
      users:
      - ${PROJECT_USER}
`

var roleBindingRestrictionUser = `
- apiVersion: v1
  kind: RoleBindingRestriction
  metadata:
    labels:
      app: fabric8-tenant
      provider: fabric8
      version: 2.0.85
      group: io.fabric8.tenant.packages
    name: dsaas-user-access
    namespace: ${USER_NAME}
  spec:
    userrestriction:
      users:
      - ${PROJECT_USER}
`

type ServiceTestSuite struct {
	gormsupport.DBTestSuite
}

func TestService(t *testing.T) {
	suite.Run(t, &ServiceTestSuite{DBTestSuite: gormsupport.NewDBTestSuite("../config.yaml")})
}

func (s *ServiceTestSuite) TestInvokePostAndGetCallsForAllObjects() {
	// given
	defer gock.OffAll()
	config, reset := test.LoadTestConfig(s.T())
	defer reset()

	gock.New("https://raw.githubusercontent.com").
		Get("fabric8-services/fabric8-tenant/12345/environment/templates/fabric8-tenant-user.yml").
		Reply(200).
		BodyString(templateHeader + projectRequestObject + roleBindingRestrictionRun)
	gock.New("http://api.cluster1/").
		Post("/oapi/v1/projectrequests").
		Reply(200)
	gock.New("http://api.cluster1/").
		Get("/oapi/v1/projects/aslak").
		Reply(404)
	gock.New("http://api.cluster1/").
		Get("/oapi/v1/projects/aslak").
		Reply(200).
		BodyString(`{"status": {"phase":"Active"}}`)
	gock.New("http://api.cluster1/").
		Post("/oapi/v1/namespaces/aslak/rolebindingrestrictions").
		Reply(200)
	gock.New("http://api.cluster1/").
		Delete("/oapi/v1/namespaces/aslak/rolebindings/admin").
		Reply(200)

	tnnt := tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(tf.SingleWithName("aslak")), tf.AddNamespaces()).Tenants[0]
	service := testdoubles.NewOSService(
		config,
		testdoubles.AddUser("aslak").
			WithData(testdoubles.NewUserDataWithTenantConfig("", "12345", "")).
			WithToken("abc123"),
		tenant.NewTenantRepository(s.DB, tnnt.ID))

	// when
	err := service.Create([]environment.Type{environment.TypeUser}, openshift.CreateOpts().EnableSelfHealing())

	// then
	require.NoError(s.T(), err)
	assertion.AssertTenantFromDB(s.T(), s.DB, tnnt.ID).
		HasNumberOfNamespaces(1).
		HasNamespaceOfTypeThat(environment.TypeUser).
		HasName("aslak").
		HasState(tenant.Ready)
}

func (s *ServiceTestSuite) TestInvokePostAndGetCallsForAllObjectsWhen403IsReturnedForTheFirstGetCall() {
	// given
	defer gock.OffAll()
	config, reset := test.LoadTestConfig(s.T())
	defer reset()

	gock.New("https://raw.githubusercontent.com").
		Get("fabric8-services/fabric8-tenant/12345/environment/templates/fabric8-tenant-user.yml").
		Reply(200).
		BodyString(templateHeader + projectRequestObject + roleBindingRestrictionRun)
	gock.New("http://api.cluster1/").
		Post("/oapi/v1/projectrequests").
		Reply(200)
	gock.New("http://api.cluster1/").
		Get("/oapi/v1/projects/aslak").
		Reply(403)
	gock.New("http://api.cluster1/").
		Get("/oapi/v1/projects/aslak").
		Reply(200).
		BodyString(`{"status": {"phase":"Active"}}`)
	gock.New("http://api.cluster1/").
		Post("/oapi/v1/namespaces/aslak/rolebindingrestrictions").
		Reply(200)
	gock.New("http://api.cluster1/").
		Delete("/oapi/v1/namespaces/aslak/rolebindings/admin").
		Reply(200)

	tnnt := tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(tf.SingleWithName("aslak")), tf.AddNamespaces()).Tenants[0]
	service := testdoubles.NewOSService(
		config,
		testdoubles.AddUser("aslak").
			WithData(testdoubles.NewUserDataWithTenantConfig("", "12345", "")).
			WithToken("abc123"),
		tenant.NewTenantRepository(s.DB, tnnt.ID))

	// when
	err := service.Create([]environment.Type{environment.TypeUser}, openshift.CreateOpts().EnableSelfHealing())

	// then
	require.NoError(s.T(), err)
	assertion.AssertTenantFromDB(s.T(), s.DB, tnnt.ID).
		HasNumberOfNamespaces(1).
		HasNamespaceOfTypeThat(environment.TypeUser).
		HasName("aslak").
		HasState(tenant.Ready)
}

func (s *ServiceTestSuite) TestUsesCorrectTokensForUserNamespace() {
	// given
	defer gock.OffAll()
	config, reset := test.LoadTestConfig(s.T())
	defer reset()

	gock.New("https://raw.githubusercontent.com").
		Get("fabric8-services/fabric8-tenant/12345/environment/templates/fabric8-tenant-user.yml").
		Reply(200).
		BodyString(templateHeader + projectRequestUser + roleBindingRestrictionUser)
	gock.New("http://api.cluster1/").
		Get("/oapi/v1/projects/john").
		SetMatcher(test.ExpectRequest(test.HasJWTWithSub("devtools-sre"))).
		Reply(404)
	gock.New("http://api.cluster1/").
		Post("/oapi/v1/projectrequests").
		SetMatcher(test.ExpectRequest(test.HasBearerWithSub("abc123"))).
		Reply(200)
	gock.New("http://api.cluster1/").
		Get("/oapi/v1/projects/john").
		SetMatcher(test.ExpectRequest(test.HasBearerWithSub("abc123"))).
		Reply(200).
		BodyString(`{"status": {"phase":"Active"}}`)
	gock.New("http://api.cluster1/").
		Post("/oapi/v1/namespaces/john/rolebinding").
		SetMatcher(test.ExpectRequest(test.HasBearerWithSub("abc123"))).
		Reply(200)
	gock.New("http://api.cluster1/").
		Post("/oapi/v1/namespaces/john/rolebindingrestrictions").
		SetMatcher(test.ExpectRequest(test.HasJWTWithSub("devtools-sre"))).
		Reply(200)
	gock.New("http://api.cluster1/").
		Delete("/oapi/v1/namespaces/john/rolebindings/admin").
		SetMatcher(test.ExpectRequest(test.HasJWTWithSub("devtools-sre"))).
		Reply(200)

	tnnt := tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(tf.SingleWithName("john")), tf.AddNamespaces()).Tenants[0]
	service := testdoubles.NewOSService(
		config,
		testdoubles.AddUser("john").
			WithData(testdoubles.NewUserDataWithTenantConfig("", "12345", "")).
			WithToken("abc123"),
		tenant.NewTenantRepository(s.DB, tnnt.ID))

	// when
	err := service.Create([]environment.Type{environment.TypeUser}, openshift.CreateOpts().EnableSelfHealing())

	// then
	require.NoError(s.T(), err)
	assertion.AssertTenantFromDB(s.T(), s.DB, tnnt.ID).
		HasNumberOfNamespaces(1).
		HasNamespaceOfTypeThat(environment.TypeUser).
		HasName("john").
		HasState(tenant.Ready)
}

func (s *ServiceTestSuite) TestDeleteIfThereIsConflict() {
	// given
	defer gock.OffAll()
	config, reset := test.LoadTestConfig(s.T())
	defer reset()

	gock.New("https://raw.githubusercontent.com").
		Get("fabric8-services/fabric8-tenant/12345/environment/templates/fabric8-tenant-user.yml").
		Reply(200).
		BodyString(templateHeader + roleBindingRestrictionRun)
	gock.New("http://api.cluster1/").
		Post("/oapi/v1/namespaces/aslak/rolebindingrestrictions").
		Reply(409)
	gock.New("http://api.cluster1/").
		Delete("/oapi/v1/namespaces/aslak/rolebindingrestrictions/dsaas-user-access").
		Reply(200)
	gock.New("http://api.cluster1/").
		Post("/oapi/v1/namespaces/aslak/rolebindingrestrictions").
		Reply(200)
	gock.New("http://api.cluster1/").
		Get("/oapi/v1/namespaces/aslak/rolebindingrestrictions/dsaas-user-access").
		Reply(200).
		BodyString(roleBindingRestrictionRun)
	gock.New("http://api.cluster1/").
		Delete("/oapi/v1/namespaces/aslak/rolebindings/admin").
		Reply(200)

	tnnt := tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(tf.SingleWithName("aslak")), tf.AddNamespaces()).Tenants[0]
	service := testdoubles.NewOSService(
		config,
		testdoubles.AddUser("aslak").
			WithData(testdoubles.NewUserDataWithTenantConfig("", "12345", "")).
			WithToken("abc123"),
		tenant.NewTenantRepository(s.DB, tnnt.ID))

	// when
	err := service.Create([]environment.Type{environment.TypeUser}, openshift.CreateOpts().EnableSelfHealing())

	// then
	require.NoError(s.T(), err)
	assertion.AssertTenantFromDB(s.T(), s.DB, tnnt.ID).
		HasNumberOfNamespaces(1).
		HasNamespaceOfTypeThat(environment.TypeUser).
		HasName("aslak").
		HasState(tenant.Ready)
}

func (s *ServiceTestSuite) TestDeleteAndGet() {
	// given
	defer gock.OffAll()
	config, reset := test.LoadTestConfig(s.T())
	defer reset()

	gock.New("https://raw.githubusercontent.com").
		Get("fabric8-services/fabric8-tenant/12345/environment/templates/fabric8-tenant-user.yml").
		Reply(200).
		BodyString(templateHeader + projectRequestObject)
	gock.New("http://api.cluster1/").
		Delete("/oapi/v1/projects/aslak").
		SetMatcher(test.ExpectRequest(test.HasJWTWithSub("devtools-sre"))).
		Reply(200)

	fxt := tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(tf.SingleWithName("aslak")), tf.AddNamespaces(environment.TypeUser))
	tnnt := fxt.Tenants[0]
	service := testdoubles.NewOSService(
		config,
		testdoubles.AddUser("aslak").
			WithData(testdoubles.NewUserDataWithTenantConfig("", "12345", "")).
			WithToken("abc123"),
		tenant.NewTenantRepository(s.DB, tnnt.ID))

	// when
	err := service.Delete(environment.DefaultEnvTypes, fxt.Namespaces, openshift.DeleteOpts().EnableSelfHealing().RemoveFromCluster())

	// then
	require.NoError(s.T(), err)
	assertion.AssertTenantFromDB(s.T(), s.DB, tnnt.ID).
		DoesNotExist().
		HasNoNamespace()
}

func (s *ServiceTestSuite) TestClean() {
	// given
	defer gock.OffAll()
	config, reset := test.LoadTestConfig(s.T())
	defer reset()
	okCalls := 0
	gock.New(test.ClusterURL).
		Get("/api/v1/namespaces/john-che/pods/first-item").
		SetMatcher(test.SpyOnCalls(&okCalls)).
		Times(2).
		Reply(200)

	testdoubles.MockCleanRequestsToOS(ptr.Int(0), test.ClusterURL)
	fxt := tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(tf.SingleWithName("john")), tf.AddDefaultNamespaces())
	tnnt := fxt.Tenants[0]
	service := testdoubles.NewOSService(
		config,
		testdoubles.AddUser("john").WithToken("abc123"),
		tenant.NewTenantRepository(s.DB, tnnt.ID))

	// when
	err := service.Delete(environment.DefaultEnvTypes, fxt.Namespaces, openshift.DeleteOpts().EnableSelfHealing())

	// then
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 2, okCalls)
	assertion.AssertTenantFromDB(s.T(), s.DB, tnnt.ID).
		Exists()
}

func (s *ServiceTestSuite) TestCleanWhenGetReturnsSomeServices() {
	// given
	defer gock.OffAll()
	config, reset := test.LoadTestConfig(s.T())
	defer reset()

	firstCoolSvcCall := 0
	secondCoolSvcCall := 0

	gock.New(test.ClusterURL).
		Get("/api/v1/namespaces/john-che/services").
		Reply(200).
		BodyString(`{"items": [
        {"metadata": {"name": "my-first-cool-service"}},
        {"metadata": {"name": "my-second-cool-service"}}]}`)
	gock.New(test.ClusterURL).
		Delete("/api/v1/namespaces/john-che/services/my-first-cool-service").
		SetMatcher(test.SpyOnCalls(&firstCoolSvcCall)).
		Reply(200).
		BodyString("{}")
	gock.New(test.ClusterURL).
		Delete("/api/v1/namespaces/john-che/services/my-second-cool-service").
		SetMatcher(test.SpyOnCalls(&secondCoolSvcCall)).
		Reply(200).
		BodyString("{}")

	testdoubles.MockCleanRequestsToOS(ptr.Int(0), test.ClusterURL)
	fxt := tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(tf.SingleWithName("john")), tf.AddDefaultNamespaces())
	tnnt := fxt.Tenants[0]
	service := testdoubles.NewOSService(
		config,
		testdoubles.AddUser("john").WithToken("abc123"),
		tenant.NewTenantRepository(s.DB, tnnt.ID))

	// when
	err := service.Delete(environment.DefaultEnvTypes, fxt.Namespaces, openshift.DeleteOpts().EnableSelfHealing())

	// then
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 1, firstCoolSvcCall)
	assert.Equal(s.T(), 1, secondCoolSvcCall)
	assertion.AssertTenantFromDB(s.T(), s.DB, tnnt.ID).
		Exists()
}

func (s *ServiceTestSuite) TestCleanWhenGetForServicesReturns500() {
	// given
	defer gock.OffAll()
	config, reset := test.LoadTestConfig(s.T())
	defer reset()

	gock.New(test.ClusterURL).
		Get("/api/v1/namespaces/john-che/services").
		Persist().
		Reply(500)
	fxt := tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(tf.SingleWithName("john")), tf.AddDefaultNamespaces())
	service := testdoubles.NewOSService(
		config,
		testdoubles.AddUser("john").WithToken("abc123"),
		tenant.NewTenantRepository(s.DB, fxt.Tenants[0].ID))

	// when
	err := service.Delete(environment.DefaultEnvTypes, fxt.Namespaces, openshift.DeleteOpts().EnableSelfHealing())

	// then
	test.AssertError(s.T(), err, test.HasMessageContaining("the method DELETE failed for the cluster"),
		test.HasMessageContaining("while getting list of objects to apply"))
}

func (s *ServiceTestSuite) TestCleanReturns404() {
	// given
	defer gock.OffAll()
	config, reset := test.LoadTestConfig(s.T())
	defer reset()

	gock.New(test.ClusterURL).
		Delete("").
		Persist().
		Reply(404).
		BodyString("{}")
	gock.New(test.ClusterURL).
		Get(`/.+/namespaces/john[^/]*/[^/]+/$`).
		Persist().
		Reply(200).
		BodyString(`{"items": []}`)
	gock.New(test.ClusterURL).
		Get(`.*\/(persistentvolumeclaims)\/.*`).
		Persist().
		Reply(404)
	fxt := tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(tf.SingleWithName("john")), tf.AddDefaultNamespaces())
	tnnt := fxt.Tenants[0]
	service := testdoubles.NewOSService(
		config,
		testdoubles.AddUser("john").WithToken("abc123"),
		tenant.NewTenantRepository(s.DB, tnnt.ID))

	// when
	err := service.Delete(environment.DefaultEnvTypes, fxt.Namespaces, openshift.DeleteOpts().EnableSelfHealing())

	// then
	require.NoError(s.T(), err)
	assertion.AssertTenantFromDB(s.T(), s.DB, tnnt.ID).
		Exists()
}

func (s *ServiceTestSuite) TestCleanReturns404WhenGatheringAvailableObjects() {
	// given
	defer gock.OffAll()
	config, reset := test.LoadTestConfig(s.T())
	defer reset()

	gock.New(test.ClusterURL).
		Delete("").
		Persist().
		Reply(202).
		BodyString("{}")
	gock.New(test.ClusterURL).
		Get(`/.+/namespaces/john[^/]*/[^/]+/$`).
		Persist().
		Reply(404).
		BodyString(`{"items": []}`)
	gock.New(test.ClusterURL).
		Get(`.*\/(persistentvolumeclaims)\/.*`).
		Persist().
		Reply(404)
	fxt := tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(tf.SingleWithName("john")), tf.AddDefaultNamespaces())
	tnnt := fxt.Tenants[0]
	service := testdoubles.NewOSService(
		config,
		testdoubles.AddUser("john").WithToken("abc123"),
		tenant.NewTenantRepository(s.DB, tnnt.ID))

	// when
	err := service.Delete(environment.DefaultEnvTypes, fxt.Namespaces, openshift.DeleteOpts().EnableSelfHealing())

	// then
	require.NoError(s.T(), err)
	assertion.AssertTenantFromDB(s.T(), s.DB, tnnt.ID).
		Exists()
}

func (s *ServiceTestSuite) TestNumberOfCallsToCluster() {
	// given
	defer gock.OffAll()
	config, reset := test.LoadTestConfig(s.T())
	defer reset()
	testdoubles.SetTemplateVersions()

	calls := 0
	testdoubles.MockPostRequestsToOS(&calls, test.ClusterURL, environment.DefaultEnvTypes, "developer")
	userCreator := testdoubles.AddUser("developer").WithToken("12345")

	tnnt := tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(tf.SingleWithName("developer")), tf.AddNamespaces()).Tenants[0]
	service := testdoubles.NewOSService(
		config,
		userCreator,
		tenant.NewTenantRepository(s.DB, tnnt.ID))

	// when
	err := service.Create(environment.DefaultEnvTypes, openshift.CreateOpts().EnableSelfHealing())

	// then
	require.NoError(s.T(), err)
	assert.Equal(s.T(), testdoubles.ExpectedNumberOfCallsWhenPost(s.T(), config), calls)
	namespaces, err := tenant.NewTenantRepository(s.DB, tnnt.ID).GetNamespaces()
	require.NoError(s.T(), err)
	assert.Len(s.T(), namespaces, len(environment.DefaultEnvTypes))
}

func (s *ServiceTestSuite) TestCreateNewNamespacesWithBaseNameEnding2WhenConflictsWithProject() {
	// given
	config, reset := test.LoadTestConfig(s.T())
	defer func() {
		gock.OffAll()
		reset()
	}()
	fxt := tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(tf.SingleWithName("johndoe")), tf.AddNamespaces())
	deleteCalls := 0

	gock.New("http://api.cluster1").
		Get("/oapi/v1/projects/johndoe-che").
		Reply(200).
		BodyString("{}")
	testdoubles.MockPostRequestsToOS(ptr.Int(0), test.ClusterURL, environment.DefaultEnvTypes, "johndoe")
	gock.New("http://api.cluster1").
		Delete("/oapi/v1/projects/.*").
		SetMatcher(test.SpyOnCalls(&deleteCalls)).
		Times(5).
		Reply(200).
		BodyString("{}")
	testdoubles.MockPostRequestsToOS(ptr.Int(0), test.ClusterURL, environment.DefaultEnvTypes, "johndoe2")

	repo := tenant.NewTenantRepository(s.DB, fxt.Tenants[0].ID)
	service := testdoubles.NewOSService(
		config,
		testdoubles.AddUser("johndoe").WithToken("12345"),
		repo)

	// when
	err := service.Create(environment.DefaultEnvTypes, openshift.CreateOpts().EnableSelfHealing())

	// then
	require.NoError(s.T(), err)
	assert.Equal(s.T(), len(environment.DefaultEnvTypes), deleteCalls)
	assertion.AssertTenant(s.T(), repo).
		HasNsBaseName("johndoe2").
		HasNumberOfNamespaces(len(environment.DefaultEnvTypes))
}

func (s *ServiceTestSuite) TestCreateNewNamespacesWithBaseNameEnding3WhenConflictsWithProjectAndWith2Exists() {
	// given
	config, reset := test.LoadTestConfig(s.T())
	defer func() {
		gock.OffAll()
		reset()
	}()
	fxt := tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(tf.SingleWithName("johndoe"), tf.SingleWithName("johndoe2")), tf.AddNamespaces())
	deleteCalls := 0

	gock.New("http://api.cluster1").
		Get("/oapi/v1/projects/johndoe-che").
		Reply(200).
		BodyString("{}")
	testdoubles.MockPostRequestsToOS(ptr.Int(0), test.ClusterURL, environment.DefaultEnvTypes, "johndoe")
	gock.New("http://api.cluster1").
		Delete("/oapi/v1/projects/.*").
		SetMatcher(test.SpyOnCalls(&deleteCalls)).
		Times(5).
		Reply(200).
		BodyString("{}")
	testdoubles.MockPostRequestsToOS(ptr.Int(0), test.ClusterURL, environment.DefaultEnvTypes, "johndoe3")

	repo := tenant.NewTenantRepository(s.DB, fxt.Tenants[0].ID)
	service := testdoubles.NewOSService(
		config,
		testdoubles.AddUser("johndoe").WithToken("12345"),
		repo)

	// when
	err := service.Create(environment.DefaultEnvTypes, openshift.CreateOpts().EnableSelfHealing())

	// then
	require.NoError(s.T(), err)
	assert.Equal(s.T(), len(environment.DefaultEnvTypes), deleteCalls)
	assertion.AssertTenant(s.T(), repo).
		HasNsBaseName("johndoe3").
		HasNumberOfNamespaces(len(environment.DefaultEnvTypes))
}

func (s *ServiceTestSuite) TestCreateNewNamespacesWithNormalBaseNameWhenFailsLimitRangesReturnsConflict() {
	// given
	defer gock.OffAll()
	config, reset := test.LoadTestConfig(s.T())
	defer reset()
	testdoubles.SetTemplateVersions()

	deleteCalls := 0
	gock.New(test.ClusterURL).
		Post("/api/v1/namespaces/johndoe-che/limitranges").
		Reply(409).
		BodyString("{}")
	gock.New(test.ClusterURL).
		Delete("/api/v1/namespaces/johndoe-che/limitranges/resource-limits").
		SetMatcher(test.SpyOnCalls(&deleteCalls)).
		Times(1).
		Reply(200).
		BodyString("{}")
	calls := 0
	testdoubles.MockPostRequestsToOS(&calls, test.ClusterURL, environment.DefaultEnvTypes, "johndoe")
	userCreator := testdoubles.AddUser("johndoe").WithToken("12345")

	tnnt := tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(tf.SingleWithName("johndoe")), tf.AddNamespaces()).Tenants[0]
	service := testdoubles.NewOSService(
		config,
		userCreator,
		tenant.NewTenantRepository(s.DB, tnnt.ID))

	// when
	err := service.Create(environment.DefaultEnvTypes, openshift.CreateOpts().EnableSelfHealing())

	// then
	require.NoError(s.T(), err)
	assert.Equal(s.T(), testdoubles.ExpectedNumberOfCallsWhenPost(s.T(), config), calls)
	assert.Equal(s.T(), 1, deleteCalls)
	namespaces, err := tenant.NewTenantRepository(s.DB, tnnt.ID).GetNamespaces()
	require.NoError(s.T(), err)
	assert.Len(s.T(), namespaces, len(environment.DefaultEnvTypes))
}

func (s *ServiceTestSuite) TestCreateNewNamespacesWithNormalBaseNameWhenFailsResourceQuotasReturnsConflict() {
	// given
	defer gock.OffAll()
	config, reset := test.LoadTestConfig(s.T())
	defer reset()
	testdoubles.SetTemplateVersions()

	deleteCalls := 0
	gock.New(test.ClusterURL).
		Post("/api/v1/namespaces/johndoe-che/resourcequotas").
		Reply(409).
		BodyString("{}")
	gock.New(test.ClusterURL).
		Delete("/api/v1/namespaces/johndoe-che/resourcequotas/.+").
		SetMatcher(test.SpyOnCalls(&deleteCalls)).
		Times(1).
		Reply(200).
		BodyString("{}")
	calls := 0
	testdoubles.MockPostRequestsToOS(&calls, test.ClusterURL, environment.DefaultEnvTypes, "johndoe")
	userCreator := testdoubles.AddUser("johndoe").WithToken("12345")

	tnnt := tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(tf.SingleWithName("johndoe")), tf.AddNamespaces()).Tenants[0]
	service := testdoubles.NewOSService(
		config,
		userCreator,
		tenant.NewTenantRepository(s.DB, tnnt.ID))

	// when
	err := service.Create(environment.DefaultEnvTypes, openshift.CreateOpts().EnableSelfHealing())

	// then
	require.NoError(s.T(), err)
	assert.Equal(s.T(), testdoubles.ExpectedNumberOfCallsWhenPost(s.T(), config)+1, calls)
	assert.Equal(s.T(), 1, deleteCalls)
	namespaces, err := tenant.NewTenantRepository(s.DB, tnnt.ID).GetNamespaces()
	require.NoError(s.T(), err)
	assert.Len(s.T(), namespaces, len(environment.DefaultEnvTypes))
}
