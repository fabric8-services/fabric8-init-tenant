package controller_test

import (
	"context"
	"fmt"
	"testing"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/fabric8-services/fabric8-tenant/app/test"
	"github.com/fabric8-services/fabric8-tenant/cluster"
	"github.com/fabric8-services/fabric8-tenant/configuration"
	"github.com/fabric8-services/fabric8-tenant/controller"
	"github.com/fabric8-services/fabric8-tenant/openshift"
	"github.com/fabric8-services/fabric8-tenant/tenant"
	testsupport "github.com/fabric8-services/fabric8-tenant/test"
	"github.com/fabric8-services/fabric8-tenant/test/gormsupport"
	"github.com/fabric8-services/fabric8-tenant/test/recorder"
	"github.com/fabric8-services/fabric8-tenant/test/testfixture"
	"github.com/fabric8-services/fabric8-tenant/token"
	"github.com/fabric8-services/fabric8-tenant/user"
	"github.com/fabric8-services/fabric8-wit/errors"
	"github.com/fabric8-services/fabric8-wit/resource"
	"github.com/goadesign/goa"
	goajwt "github.com/goadesign/goa/middleware/security/jwt"
	"github.com/jinzhu/gorm"
	errs "github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type TenantsControllerTestSuite struct {
	gormsupport.DBTestSuite
}

func TestTenantsController(t *testing.T) {
	resource.Require(t, resource.Database)
	suite.Run(t, &TenantsControllerTestSuite{DBTestSuite: gormsupport.NewDBTestSuite("../config.yaml")})
}

var resolveCluster = func(ctx context.Context, target string) (*cluster.Cluster, error) {
	return &cluster.Cluster{
		APIURL:     "https://api.example.com",
		ConsoleURL: "https://console.example.com/console",
		MetricsURL: "https://metrics.example.com",
		LoggingURL: "https://console.example.com/console", // not a typo; logging and console are on the same host
		AppDNS:     "apps.example.com",
		User:       "service-account",
		Token:      "XX",
	}, nil
}

func (s *TenantsControllerTestSuite) TestShowTenants() {

	// given
	svc, ctrl, err := newTestTenantsController(s.DB, "show-tenants")
	require.NoError(s.T(), err)

	s.T().Run("OK", func(t *testing.T) {
		// given
		fxt := testfixture.NewTestFixture(t, s.DB, testfixture.Tenants(1), testfixture.Namespaces(1))
		// when
		_, tenant := test.ShowTenantsOK(t, createValidSAContext("fabric8-jenkins-idler"), svc, ctrl, fxt.Tenants[0].ID)
		// then
		assert.Equal(t, fxt.Tenants[0].ID, *tenant.Data.ID)
		assert.Equal(t, 1, len(tenant.Data.Attributes.Namespaces))
	})

	s.T().Run("Failures", func(t *testing.T) {

		t.Run("Unauhorized - no token", func(t *testing.T) {
			// when/then
			test.ShowTenantsUnauthorized(t, context.Background(), svc, ctrl, uuid.NewV4())
		})

		t.Run("Unauhorized - no SA token", func(t *testing.T) {
			// when/then
			test.ShowTenantsUnauthorized(t, createInvalidSAContext(), svc, ctrl, uuid.NewV4())
		})

		t.Run("Unauhorized - wrong SA token", func(t *testing.T) {
			// when/then
			test.ShowTenantsUnauthorized(t, createValidSAContext("other service account"), svc, ctrl, uuid.NewV4())
		})

		t.Run("Not found", func(t *testing.T) {
			// when/then
			test.ShowTenantsNotFound(t, createValidSAContext("fabric8-jenkins-idler"), svc, ctrl, uuid.NewV4())
		})
	})
}

func (s *TenantsControllerTestSuite) TestSearchTenants() {

	// given
	svc, ctrl, err := newTestTenantsController(s.DB, "search-tenants")
	require.NoError(s.T(), err)

	s.T().Run("OK", func(t *testing.T) {
		// given
		fxt := testfixture.NewTestFixture(t, s.DB, testfixture.Tenants(1), testfixture.Namespaces(1))
		// when
		_, tenant := test.SearchTenantsOK(t, createValidSAContext("fabric8-jenkins-idler"), svc, ctrl, fxt.Namespaces[0].MasterURL, fxt.Namespaces[0].Name)
		// then
		require.Len(t, tenant.Data, 1)
		assert.Equal(t, fxt.Tenants[0].ID, *tenant.Data[0].ID)
		assert.Equal(t, 1, len(tenant.Data[0].Attributes.Namespaces))
	})

	s.T().Run("Failures", func(t *testing.T) {

		t.Run("Unauhorized - no token", func(t *testing.T) {
			test.SearchTenantsUnauthorized(t, context.Background(), svc, ctrl, "foo", "bar")
		})

		t.Run("Unauhorized - no SA token", func(t *testing.T) {
			test.SearchTenantsUnauthorized(t, createInvalidSAContext(), svc, ctrl, "foo", "bar")
		})

		t.Run("Unauhorized - wrong SA token", func(t *testing.T) {
			test.SearchTenantsUnauthorized(t, createValidSAContext("other service account"), svc, ctrl, "foo", "bar")
		})

		t.Run("Not found", func(t *testing.T) {
			test.SearchTenantsNotFound(t, createValidSAContext("fabric8-jenkins-idler"), svc, ctrl, "foo", "bar")
		})
	})
}

func (s *TenantsControllerTestSuite) TestDeleteTenants() {

	s.T().Run("Success", func(t *testing.T) {

		t.Run("all ok", func(t *testing.T) {
			// given
			fxt := testfixture.NewTestFixture(t, s.DB, testfixture.Tenants(1, func(fxt *testfixture.TestFixture, idx int) error {
				id, err := uuid.FromString("8c97b9fc-2a3f-4bef-8579-75e676ab1348") // force the ID to match the go-vcr cassette in the `delete-tenants.yaml` file
				if err != nil {
					return err
				}
				fxt.Tenants[0].ID = id
				return nil
			}), testfixture.Namespaces(2, func(fxt *testfixture.TestFixture, idx int) error {
				fxt.Namespaces[idx].TenantID = fxt.Tenants[0].ID
				fxt.Namespaces[idx].MasterURL = "https://api.cluster1"
				if idx == 0 {
					fxt.Namespaces[idx].Name = "foo"
					fxt.Namespaces[idx].Type = "user"
				} else if idx == 1 {
					fxt.Namespaces[idx].Name = "foo-che"
					fxt.Namespaces[idx].Type = "che"
				}
				return nil
			}))
			svc, ctrl, err := newTestTenantsController(s.DB, "delete-tenants-204")
			require.NoError(t, err)
			// when
			test.DeleteTenantsNoContent(t, createValidSAContext("fabric8-auth"), svc, ctrl, fxt.Tenants[0].ID)
			// then
			_, err = tenant.NewDBService(s.DB).GetTenant(fxt.Tenants[0].ID)
			require.IsType(t, errors.NotFoundError{}, err)
			namespaces, err := tenant.NewDBService(s.DB).GetNamespaces(fxt.Tenants[0].ID)
			require.NoError(t, err)
			assert.Empty(t, namespaces)
		})

		t.Run("ok even if namespace missing", func(t *testing.T) {
			// if the namespace record exist in the DB, but the `delete namespace` call on the cluster endpoint fails with a 404
			// given
			fxt := testfixture.NewTestFixture(t, s.DB, testfixture.Tenants(1, func(fxt *testfixture.TestFixture, idx int) error {
				id, err := uuid.FromString("0257147d-0bb8-4624-a054-853e49c97d07") // force the ID to match the go-vcr cassette in the `delete-tenants.yaml` file
				if err != nil {
					return err
				}
				fxt.Tenants[0].ID = id
				return nil
			}), testfixture.Namespaces(2, func(fxt *testfixture.TestFixture, idx int) error {
				fxt.Namespaces[idx].TenantID = fxt.Tenants[0].ID
				fxt.Namespaces[idx].MasterURL = "https://api.cluster1"
				if idx == 0 {
					fxt.Namespaces[idx].Name = "bar"
					fxt.Namespaces[idx].Type = "user"
				} else if idx == 1 {
					fxt.Namespaces[idx].Name = "bar-che"
					fxt.Namespaces[idx].Type = "che"
				}
				return nil
			}))
			svc, ctrl, err := newTestTenantsController(s.DB, "delete-tenants-403")
			require.NoError(t, err)
			// when
			test.DeleteTenantsNoContent(t, createValidSAContext("fabric8-auth"), svc, ctrl, fxt.Tenants[0].ID)
			// then
			_, err = tenant.NewDBService(s.DB).GetTenant(fxt.Tenants[0].ID)
			require.IsType(t, errors.NotFoundError{}, err)
			namespaces, err := tenant.NewDBService(s.DB).GetNamespaces(fxt.Tenants[0].ID)
			require.NoError(t, err)
			assert.Empty(t, namespaces)
		})

	})

	s.T().Run("Failures", func(t *testing.T) {

		svc, ctrl, err := newTestTenantsController(s.DB, "delete-tenants-204")
		require.NoError(t, err)

		t.Run("Unauhorized - no token", func(t *testing.T) {
			// when/then
			test.DeleteTenantsUnauthorized(t, context.Background(), svc, ctrl, uuid.NewV4())
		})

		t.Run("Unauhorized - no SA token", func(t *testing.T) {
			// when/then
			test.DeleteTenantsUnauthorized(t, createInvalidSAContext(), svc, ctrl, uuid.NewV4())
		})

		t.Run("Unauhorized - wrong SA token", func(t *testing.T) {
			// when/then
			test.DeleteTenantsUnauthorized(t, createValidSAContext("other service account"), svc, ctrl, uuid.NewV4())
		})

		t.Run("namespace deletion failed", func(t *testing.T) {
			// case where the first namespace could not be deleted: the tenant and the namespaces should still be in the DB
			// given
			fxt := testfixture.NewTestFixture(t, s.DB, testfixture.Tenants(1, func(fxt *testfixture.TestFixture, idx int) error {
				id, err := uuid.FromString("5a95c51b-120a-4d03-b529-98bd7d4a5689") // force the ID to match the go-vcr cassette in the `delete-tenants.yaml` file
				if err != nil {
					return err
				}
				fxt.Tenants[0].ID = id
				return nil
			}), testfixture.Namespaces(2, func(fxt *testfixture.TestFixture, idx int) error {
				fxt.Namespaces[idx].TenantID = fxt.Tenants[0].ID
				fxt.Namespaces[idx].MasterURL = "https://api.cluster1"
				if idx == 0 {
					fxt.Namespaces[idx].Name = "baz"
					fxt.Namespaces[idx].Type = "user"
				} else if idx == 1 {
					fxt.Namespaces[idx].TenantID = fxt.Tenants[0].ID
					fxt.Namespaces[idx].Name = "baz-che"
					fxt.Namespaces[idx].Type = "che"
				}
				return nil
			}))
			svc, ctrl, err := newTestTenantsController(s.DB, "delete-tenants-500")
			require.NoError(t, err)
			// when
			test.DeleteTenantsInternalServerError(t, createValidSAContext("fabric8-auth"), svc, ctrl, fxt.Tenants[0].ID)
			// then
			_, err = tenant.NewDBService(s.DB).GetTenant(fxt.Tenants[0].ID)
			require.NoError(t, err)
			namespaces, err := tenant.NewDBService(s.DB).GetNamespaces(fxt.Tenants[0].ID)
			require.NoError(t, err)
			require.Len(t, namespaces, 2)
			// firs namespace could not be deleted, both still exist in the DB (and in the cluster)
			assert.Equal(t, "baz", namespaces[0].Name)
			assert.Equal(t, "baz-che", namespaces[1].Name)
		})
	})

}

func createValidSAContext(sub string) context.Context {
	claims := jwt.MapClaims{}
	claims["service_accountname"] = sub
	token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
	return goajwt.WithJWT(context.Background(), token)
}

func createInvalidSAContext() context.Context {
	claims := jwt.MapClaims{}
	token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
	return goajwt.WithJWT(context.Background(), token)
}

func newTestTenantsController(db *gorm.DB, filename string) (*goa.Service, *controller.TenantsController, error) {
	r, err := recorder.New(fmt.Sprintf("../test/data/controller/%s", filename), recorder.WithJWTMatcher())
	if err != nil {
		return nil, nil, errs.Wrapf(err, "unable to initialize tenant controller")
	}
	defer r.Stop()

	saToken, err := testsupport.NewToken(
		map[string]interface{}{
			"sub": "tenant_service",
		},
		"../test/private_key.pem",
	)
	if err != nil {
		fmt.Printf("error occurred: %v", err)
		return nil, nil, errs.Wrapf(err, "unable to initialize tenant controller")
	}

	authURL := "http://authservice"
	resolveToken := token.NewResolve(authURL, configuration.WithRoundTripper(r.Transport))
	clusterService := cluster.NewService(
		authURL,
		saToken.Raw,
		resolveToken,
		token.NewGPGDecypter("foo"),
		configuration.WithRoundTripper(r.Transport),
	)
	clusters, err := clusterService.GetClusters(context.Background())
	if err != nil {
		return nil, nil, errs.Wrapf(err, "unable to initialize tenant controller")
	}
	resolveCluster := cluster.NewResolve(clusters)
	resolveTenant := func(ctx context.Context, target, userToken string) (user, accessToken string, err error) {
		// log.Debug(ctx, map[string]interface{}{"user_token": userToken}, "attempting to resolve tenant for user...")
		return resolveToken(ctx, target, userToken, false, token.PlainText) // no need to use "forcePull=true" to validate the user's token on the target.
	}
	tenantService := tenant.NewDBService(db)
	userService := user.NewService(
		authURL,
		saToken.Raw,
		configuration.WithRoundTripper(r.Transport),
	)
	openshiftService := openshift.NewService(configuration.WithRoundTripper(r.Transport))
	defaultOpenshiftConfig := openshift.Config{}
	svc := goa.New("Tenants-service")
	ctrl := controller.NewTenantsController(svc, tenantService, userService, openshiftService, resolveTenant, resolveCluster, defaultOpenshiftConfig)
	return svc, ctrl, nil
}
