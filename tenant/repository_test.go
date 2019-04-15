package tenant_test

import (
	"github.com/fabric8-services/fabric8-tenant/test/assertion"
	"sync"
	"testing"

	"fmt"
	"github.com/fabric8-services/fabric8-common/errors"
	"github.com/fabric8-services/fabric8-tenant/configuration"
	"github.com/fabric8-services/fabric8-tenant/environment"
	"github.com/fabric8-services/fabric8-tenant/tenant"
	"github.com/fabric8-services/fabric8-tenant/test"
	"github.com/fabric8-services/fabric8-tenant/test/doubles"
	"github.com/fabric8-services/fabric8-tenant/test/gormsupport"
	tf "github.com/fabric8-services/fabric8-tenant/test/testfixture"
	"github.com/jinzhu/gorm"
	"github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type TenantServiceTestSuite struct {
	gormsupport.DBTestSuite
}

func TestTenantService(t *testing.T) {
	suite.Run(t, &TenantServiceTestSuite{DBTestSuite: gormsupport.NewDBTestSuite("../config.yaml")})
}

func (s *TenantServiceTestSuite) TestCreateTenant() {

	s.T().Run("ko - duplicate", func(t *testing.T) {
		// given
		svc := tenant.NewDBService(s.DB)
		tenant := &tenant.Tenant{
			ID:      uuid.NewV4(),
			Email:   "joe@foo.com",
			Profile: "free",
		}
		// when
		err := svc.CreateTenant(tenant)
		assert.NoError(t, err)
		err = svc.CreateTenant(tenant)
		// then
		assert.Error(t, err)
	})
}

func (s *TenantServiceTestSuite) TestSaveTenant() {

	s.T().Run("ok", func(t *testing.T) {
		// given
		svc := tenant.NewDBService(s.DB)
		tenant := &tenant.Tenant{
			ID:      uuid.NewV4(),
			Email:   "joe@foo.com",
			Profile: "free",
		}
		// when
		err := svc.SaveTenant(tenant)
		// then
		assert.NoError(t, err)
	})

	s.T().Run("ko - missing id", func(t *testing.T) {
		// given
		svc := tenant.NewDBService(s.DB)
		tenant := &tenant.Tenant{
			Email:   "joe@foo.com",
			Profile: "free",
		}
		// when
		err := svc.SaveTenant(tenant)
		// then
		assert.Error(t, err)
	})

	s.T().Run("ko - invalid profile", func(t *testing.T) {
		// given
		svc := tenant.NewDBService(s.DB)
		tenant := &tenant.Tenant{
			ID:      uuid.NewV4(),
			Email:   "joe@foo.com",
			Profile: "unknown",
		}
		// when
		err := svc.SaveTenant(tenant)
		// then
		assert.Error(t, err)
	})
}

func (s *TenantServiceTestSuite) TestUpdateTenant() {

	s.T().Run("ok", func(t *testing.T) {
		// given
		fxt := tf.NewTestFixture(t, s.DB, tf.Tenants(1))
		svc := tenant.NewDBService(s.DB)
		tenant := fxt.Tenants[0]
		// when
		tenant.Email = "joe@bar.com"
		err := svc.SaveTenant(tenant)
		// then
		assert.NoError(t, err)
	})

	s.T().Run("ko - invalid profile", func(t *testing.T) {
		// given
		fxt := tf.NewTestFixture(t, s.DB, tf.Tenants(1))
		svc := tenant.NewDBService(s.DB)
		tenant := fxt.Tenants[0]
		// when
		tenant.Profile = "unknown"
		err := svc.SaveTenant(tenant)
		// then
		assert.Error(t, err)
	})
}

func (s *TenantServiceTestSuite) TestLookupTenantByNamespace() {
	s.T().Run("ok", func(t *testing.T) {
		// given
		fxt := tf.NewTestFixture(t, s.DB, tf.Tenants(1), tf.Namespaces(1))
		svc := tenant.NewDBService(s.DB)
		ns := fxt.Namespaces[0]
		// when
		result, err := svc.LookupTenantByClusterAndNamespace(ns.MasterURL, ns.Name)
		// then
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, fxt.Tenants[0].ID, result.ID)
	})

	s.T().Run("not found", func(t *testing.T) {
		// given
		svc := tenant.NewDBService(s.DB)
		// when
		result, err := svc.LookupTenantByClusterAndNamespace("foo", "bar")
		// then
		require.Error(t, err)
		require.IsType(t, errors.NotFoundError{}, err)
		require.Nil(t, result)
	})

}

func (s *TenantServiceTestSuite) TestGetAllTenantsToUpdate() {
	s.T().Run("returns all tenants", func(t *testing.T) {
		// given
		configuration.Commit = "123abc"
		testdoubles.SetTemplateVersions()
		tf.FillDB(t, s.DB, tf.AddTenants(3), tf.AddDefaultNamespaces().State(tenant.Ready).Outdated())
		svc := tenant.NewDBService(s.DB)

		// when
		result, err := svc.GetTenantsToUpdate(testdoubles.GetMappedVersions(environment.DefaultEnvTypes...), 10, "xyz", "")

		// then
		assert.NoError(t, err)
		assert.Len(t, result, 3)
	})

	s.T().Run("returns only the limited number of tenants", func(t *testing.T) {
		// given
		configuration.Commit = "123abc"
		testdoubles.SetTemplateVersions()
		tf.FillDB(t, s.DB, tf.AddTenants(10), tf.AddDefaultNamespaces().State(tenant.Ready).Outdated())
		svc := tenant.NewDBService(s.DB)

		// when
		result, err := svc.GetTenantsToUpdate(testdoubles.GetMappedVersions(environment.DefaultEnvTypes...), 5, "xyz", "")

		// then
		assert.NoError(t, err)
		assert.Len(t, result, 5)
	})
}

func (s *TenantServiceTestSuite) TestGetThreeClustersToUpdate() {
	// given
	configuration.Commit = "123abc"
	testdoubles.SetTemplateVersions()
	tf.FillDB(s.T(), s.DB, tf.AddTenants(3), tf.AddDefaultNamespaces().Outdated())
	tf.FillDB(s.T(), s.DB, tf.AddTenants(3), tf.AddDefaultNamespaces().Outdated().MasterURL("http://cool-cluster.com"))
	tf.FillDB(s.T(), s.DB, tf.AddTenants(3), tf.AddDefaultNamespaces().Outdated().MasterURL("http://my-cluster.com"))
	svc := tenant.NewDBService(s.DB)

	// when
	clusters, err := svc.GetClustersToUpdate(testdoubles.GetMappedVersions(environment.DefaultEnvTypes...), "xyz")

	// then
	assert.NoError(s.T(), err)
	assert.Len(s.T(), clusters, 3)
	assert.Contains(s.T(), clusters, test.Normalize(test.ClusterURL))
	assert.Contains(s.T(), clusters, "http://cool-cluster.com")
	assert.Contains(s.T(), clusters, "http://my-cluster.com")
}
func (s *TenantServiceTestSuite) TestGetOneClusterToUpdate() {
	// given
	configuration.Commit = "123abc"
	testdoubles.SetTemplateVersions()
	tf.FillDB(s.T(), s.DB, tf.AddTenants(3), tf.AddDefaultNamespaces().Outdated())
	tf.FillDB(s.T(), s.DB, tf.AddTenants(3), tf.AddDefaultNamespaces().MasterURL("http://cool-cluster.com"))
	tf.FillDB(s.T(), s.DB, tf.AddTenants(3), tf.AddDefaultNamespaces().MasterURL("http://my-cluster.com"))
	svc := tenant.NewDBService(s.DB)

	// when
	clusters, err := svc.GetClustersToUpdate(testdoubles.GetMappedVersions(environment.DefaultEnvTypes...), "xyz")

	// then
	assert.NoError(s.T(), err)
	assert.Len(s.T(), clusters, 1)
	assert.Contains(s.T(), clusters, test.Normalize(test.ClusterURL))
}

func (s *TenantServiceTestSuite) TestGetNoClusterToUpdate() {
	// given
	configuration.Commit = "123abc"
	testdoubles.SetTemplateVersions()
	tf.FillDB(s.T(), s.DB, tf.AddTenants(3), tf.AddDefaultNamespaces())
	tf.FillDB(s.T(), s.DB, tf.AddTenants(3), tf.AddDefaultNamespaces().MasterURL("http://cool-cluster.com"))
	tf.FillDB(s.T(), s.DB, tf.AddTenants(3), tf.AddDefaultNamespaces().MasterURL("http://my-cluster.com"))
	svc := tenant.NewDBService(s.DB)

	// when
	clusters, err := svc.GetClustersToUpdate(testdoubles.GetMappedVersions(environment.DefaultEnvTypes...), "xyz")

	// then
	assert.NoError(s.T(), err)
	assert.Empty(s.T(), clusters)
}

func (s *TenantServiceTestSuite) TestGetAllTenantsToUpdateBatchByBatch() {
	s.T().Run("will need to call GetTenantsToUpdate three times to get all tenants to update", func(t *testing.T) {
		// given
		configuration.Commit = "123abc"
		testdoubles.SetTemplateVersions()
		fxt := tf.FillDB(t, s.DB, tf.AddTenants(11), tf.AddDefaultNamespaces().State(tenant.Ready).Outdated())
		svc := tenant.NewDBService(s.DB)
		mappedVersions := testdoubles.GetMappedVersions(environment.DefaultEnvTypes...)

		// when
		firstBatch, err := svc.GetTenantsToUpdate(mappedVersions, 5, "xyz", "")

		// then
		require.NoError(t, err)
		assert.Len(t, firstBatch, 5)
		assertContentOfTenants(t, firstBatch, fxt.Tenants, true)
		updateAllTenants(t, firstBatch, svc, false)

		// and when
		secondBatch, err := svc.GetTenantsToUpdate(mappedVersions, 5, "xyz", "")

		// then
		require.NoError(t, err)
		assert.Len(t, secondBatch, 5)
		assertContentOfTenants(t, secondBatch, fxt.Tenants, true)
		assertContentOfTenants(t, secondBatch, firstBatch, false)
		updateAllTenants(t, secondBatch, svc, true)

		// and when
		thirdBatch, err := svc.GetTenantsToUpdate(mappedVersions, 5, "xyz", "")

		// then
		require.NoError(t, err)
		assert.Len(t, thirdBatch, 1)
		assertContentOfTenants(t, thirdBatch, fxt.Tenants, true)
		assertContentOfTenants(t, thirdBatch, firstBatch, false)
		assertContentOfTenants(t, thirdBatch, secondBatch, false)
		updateAllTenants(t, thirdBatch, svc, false)

		// and when
		lastBatch, err := svc.GetTenantsToUpdate(mappedVersions, 5, "xyz", "")

		// then
		require.NoError(t, err)
		assert.Len(t, lastBatch, 0)
	})
}

func updateAllTenants(t *testing.T, toUpdate []*tenant.Tenant, svc tenant.Service, failed bool) {
	mappedVersions := testdoubles.GetMappedVersions(environment.DefaultEnvTypes...)
	for _, tnnt := range toUpdate {
		repo := svc.NewTenantRepository(tnnt.ID)
		namespaces, err := repo.GetNamespaces()
		assert.NoError(t, err)
		for _, ns := range namespaces {
			if failed {
				ns.State = tenant.Failed
			} else {
				ns.Version = mappedVersions[ns.Type]
				ns.State = tenant.Ready
			}
			ns.UpdatedBy = "xyz"
			assert.NoError(t, repo.SaveNamespace(ns))
		}
	}
}

func (s *TenantServiceTestSuite) TestCreateNamespaceInParallel() {
	// given
	fxt := tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(tf.SingleWithName("johny")), tf.AddNamespaces())

	var wg sync.WaitGroup
	wg.Add(10)
	var run sync.WaitGroup
	run.Add(1)

	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			for _, envType := range environment.DefaultEnvTypes {
				ns := &tenant.Namespace{
					TenantID:  fxt.Tenants[0].ID,
					Type:      envType,
					Name:      tenant.ConstructNamespaceName(envType, "johny"),
					MasterURL: test.Normalize(test.ClusterURL),
				}

				// when
				run.Wait()
				_, err := tenant.NewTenantRepository(s.DB, fxt.Tenants[0].ID).CreateNamespace(ns)

				require.NoError(s.T(), err)
			}
		}()
	}
	run.Done()
	wg.Wait()

	// then
	assertion.AssertTenantFromDB(s.T(), s.DB, fxt.Tenants[0].ID).
		HasNumberOfNamespaces(len(environment.DefaultEnvTypes))
}

func assertContentOfTenants(t *testing.T, expectedTenants []*tenant.Tenant, slice []*tenant.Tenant, shouldContain bool) {
	for _, tnnt := range expectedTenants {
		found := false
		for _, t := range slice {
			if t.ID == tnnt.ID {
				found = true
				break
			}
		}
		if shouldContain {
			assert.Truef(t, found, "the slice %s does not contain tenant %s", slice, tnnt)
		} else {
			assert.False(t, found, "the slice %s should not contain tenant %s", slice, tnnt)
		}
	}
}

func (s *TenantServiceTestSuite) TestGetSubsetOfFailedTenantsToUpdate() {
	s.T().Run("returns only those tenants whose namespaces have different updated_by", func(t *testing.T) {
		// given
		testdoubles.SetTemplateVersions()
		configuration.Commit = "123abc"
		previouslyFailed := tf.FillDB(t, s.DB, tf.AddTenants(1), tf.AddDefaultNamespaces().State(tenant.Failed).Outdated())
		configuration.Commit = "234bcd"
		tf.FillDB(t, s.DB, tf.AddTenants(6), tf.AddDefaultNamespaces().State(tenant.Failed).Outdated())

		svc := tenant.NewDBService(s.DB)

		// when
		result, err := svc.GetTenantsToUpdate(testdoubles.GetMappedVersions(environment.DefaultEnvTypes...), 10, "234bcd", "")

		// then
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, previouslyFailed.Tenants[0].ID, result[0].ID)
	})
}

func (s *TenantServiceTestSuite) TestGetSubsetOfTenantsThatAreOutdatedToUpdate() {
	s.T().Run("returns only those tenants whose namespaces have different version", func(t *testing.T) {
		// given
		testdoubles.SetTemplateVersions()
		configuration.Commit = "123abc"
		outdated := tf.FillDB(t, s.DB, tf.AddTenants(1), tf.AddDefaultNamespaces().State(tenant.Ready).Outdated())
		tf.FillDB(t, s.DB, tf.AddTenants(6), tf.AddDefaultNamespaces().State(tenant.Ready))

		svc := tenant.NewDBService(s.DB)

		// when
		result, err := svc.GetTenantsToUpdate(testdoubles.GetMappedVersions(environment.DefaultEnvTypes...), 10, "234bcd", "")

		// then
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, outdated.Tenants[0].ID, result[0].ID)
	})
}

func (s *TenantServiceTestSuite) TestGetSubsetOfTenantsThatMatchesRequiredCluster() {
	s.T().Run("returns only those tenants whose namespaces have different version", func(t *testing.T) {
		// given
		testdoubles.SetTemplateVersions()
		configuration.Commit = "123abc"
		toBeFound := tf.FillDB(s.T(), s.DB, tf.AddTenants(1),
			tf.AddDefaultNamespaces().State(tenant.Ready).MasterURL("http://api.cluster1").Outdated())
		tf.FillDB(s.T(), s.DB, tf.AddTenants(3),
			tf.AddDefaultNamespaces().State(tenant.Ready).MasterURL("http://api.cluster2").Outdated())

		svc := tenant.NewDBService(s.DB)

		// when
		result, err := svc.GetTenantsToUpdate(testdoubles.GetMappedVersions(environment.DefaultEnvTypes...), 10, "234bcd", "http://api.cluster1")

		// then
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, toBeFound.Tenants[0].ID, result[0].ID)
	})
}

func (s *TenantServiceTestSuite) TestGetNumberOfAllOutdatedTenants() {
	// given
	configuration.Commit = "123abc"
	testdoubles.SetTemplateVersions()
	tf.FillDB(s.T(), s.DB, tf.AddTenants(3), tf.AddDefaultNamespaces().State(tenant.Ready).Outdated())
	svc := tenant.NewDBService(s.DB)

	// when
	count, err := svc.GetNumberOfOutdatedTenants(testdoubles.GetMappedVersions(environment.DefaultEnvTypes...), "xyz", "")

	// then
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), 3, count)
}

func (s *TenantServiceTestSuite) TestGetNumberOfPreviouslyFailedTenants() {
	// given
	testdoubles.SetTemplateVersions()
	configuration.Commit = "123abc"
	tf.FillDB(s.T(), s.DB, tf.AddTenants(5), tf.AddDefaultNamespaces().State(tenant.Failed).Outdated())
	configuration.Commit = "234bcd"
	tf.FillDB(s.T(), s.DB, tf.AddTenants(6), tf.AddDefaultNamespaces().State(tenant.Failed).Outdated())

	svc := tenant.NewDBService(s.DB)

	// when
	count, err := svc.GetNumberOfOutdatedTenants(testdoubles.GetMappedVersions(environment.DefaultEnvTypes...), "234bcd", "")

	// then
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 5, count)
}

func (s *TenantServiceTestSuite) TestGetNumberOfTenantsThatAreOutdated() {
	// given
	testdoubles.SetTemplateVersions()
	configuration.Commit = "123abc"
	tf.FillDB(s.T(), s.DB, tf.AddTenants(5), tf.AddDefaultNamespaces().State(tenant.Ready).Outdated())
	tf.FillDB(s.T(), s.DB, tf.AddTenants(6), tf.AddDefaultNamespaces().State(tenant.Ready))

	svc := tenant.NewDBService(s.DB)

	// when
	count, err := svc.GetNumberOfOutdatedTenants(testdoubles.GetMappedVersions(environment.DefaultEnvTypes...), "234bcd", "")

	// then
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 5, count)
}

func (s *TenantServiceTestSuite) TestGetNumberOfOutdatedTenantsLimitedToCluster() {
	// given
	testdoubles.SetTemplateVersions()
	configuration.Commit = "123abc"
	tf.FillDB(s.T(), s.DB, tf.AddTenants(5),
		tf.AddDefaultNamespaces().State(tenant.Ready).MasterURL("http://api.cluster1").Outdated())
	tf.FillDB(s.T(), s.DB, tf.AddTenants(3),
		tf.AddDefaultNamespaces().State(tenant.Ready).MasterURL("http://api.cluster2").Outdated())

	svc := tenant.NewDBService(s.DB)

	// when
	count, err := svc.GetNumberOfOutdatedTenants(testdoubles.GetMappedVersions(environment.DefaultEnvTypes...), "234bcd", "http://api.cluster1")

	// then
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 5, count)
}

func (s *TenantServiceTestSuite) TestGetNumberOfOutdatedTenantsLimitedToEnvType() {
	// given
	testdoubles.SetTemplateVersions()
	configuration.Commit = "123abc"
	tf.FillDB(s.T(), s.DB, tf.AddTenants(5), tf.AddNamespaces(environment.TypeUser, environment.TypeChe))
	tf.FillDB(s.T(), s.DB, tf.AddTenants(6), tf.AddNamespaces(environment.TypeUser, environment.TypeUser).Outdated())
	tf.FillDB(s.T(), s.DB, tf.AddTenants(4), tf.AddDefaultNamespaces().Outdated())

	svc := tenant.NewDBService(s.DB)

	// when
	count, err := svc.GetNumberOfOutdatedTenants(testdoubles.GetMappedVersions(environment.TypeUser), "234bcd", "")

	// then
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 10, count)
}

func (s *TenantServiceTestSuite) TestDeleteNamespaces() {
	s.T().Run("all info", func(t *testing.T) {
		// given
		fxt := tf.FillDB(s.T(), s.DB, tf.AddTenants(2), tf.AddDefaultNamespaces())
		repo1 := tenant.NewTenantRepository(s.DB, fxt.Tenants[0].ID)
		repo2 := tenant.NewTenantRepository(s.DB, fxt.Tenants[1].ID)
		// when
		namespaces, err := repo1.GetNamespaces()
		require.NoError(t, err)
		for _, ns := range namespaces {
			err := repo1.DeleteNamespace(ns)
			require.NoError(s.T(), err)
		}
		// then
		// should be deleted only namespaces
		assertion.AssertTenant(t, repo1).
			Exists().
			HasNoNamespace()

		// should not be deleted
		assertion.AssertTenant(t, repo2).
			Exists().
			HasNumberOfNamespaces(len(environment.DefaultEnvTypes))
	})
}

func (s *TenantServiceTestSuite) TestDeleteTenant() {
	s.T().Run("all info", func(t *testing.T) {
		// given
		fxt := tf.FillDB(s.T(), s.DB, tf.AddTenants(2), tf.AddDefaultNamespaces())
		repo1 := tenant.NewTenantRepository(s.DB, fxt.Tenants[0].ID)
		repo2 := tenant.NewTenantRepository(s.DB, fxt.Tenants[1].ID)
		// when
		err := repo1.DeleteTenant()
		require.NoError(s.T(), err)
		// then
		// should be deleted only tenant
		assertion.AssertTenant(t, repo1).
			DoesNotExist().
			HasNumberOfNamespaces(len(environment.DefaultEnvTypes))

		// should not be deleted
		assertion.AssertTenant(t, repo2).
			Exists().
			HasNumberOfNamespaces(len(environment.DefaultEnvTypes))
	})
}

func (s *TenantServiceTestSuite) TestNsBaseNameConstruction() {

	s.T().Run("is first tenant", func(t *testing.T) {
		// given
		svc := tenant.NewDBService(s.DB)
		// when
		nsBaseName, err := tenant.ConstructNsBaseName(svc, "firstjohny")
		// then
		assert.NoError(t, err)
		assert.Equal(t, "firstjohny", nsBaseName)
	})

	s.T().Run("is second tenant with the same name", func(t *testing.T) {
		// given
		tf.NewTestFixture(t, s.DB, tf.Namespaces(1, func(fxt *tf.TestFixture, idx int) error {
			fxt.Namespaces[idx].Name = "johny-che"
			return nil
		}))
		svc := tenant.NewDBService(s.DB)
		// when
		nsBaseName, err := tenant.ConstructNsBaseName(svc, "johny")
		// then
		assert.NoError(t, err)
		assert.Equal(t, "johny2", nsBaseName)
	})

	s.T().Run("is tenth tenant with the same name", func(t *testing.T) {
		// given
		tf.NewTestFixture(t, s.DB, tf.Tenants(8, func(fxt *tf.TestFixture, idx int) error {
			nsBaseName := fmt.Sprintf("johny%d", idx+2)
			fxt.Tenants[idx].NsBaseName = nsBaseName
			return nil
		}), tf.Namespaces(1, func(fxt *tf.TestFixture, idx int) error {
			fxt.Namespaces[idx] = &tenant.Namespace{Name: "johny"}
			return nil
		}))
		svc := tenant.NewDBService(s.DB)
		// when
		nsBaseName, err := tenant.ConstructNsBaseName(svc, "johny")
		// then
		assert.NoError(t, err)
		assert.Equal(t, "johny10", nsBaseName)
	})

	s.T().Run("repo returns a failure while getting tenants", func(t *testing.T) {
		// given
		svc := serviceWithFailures{
			Service:      tenant.NewDBService(s.DB),
			errsToReturn: &[]error{gorm.ErrInvalidSQL},
		}
		// when
		_, err := tenant.ConstructNsBaseName(svc, "failingJohny")
		// then
		test.AssertError(t, err,
			test.HasMessageContaining("getting already existing tenants with the NsBaseName failingJohny failed"),
			test.IsOfType(gorm.ErrInvalidSQL))
	})

	s.T().Run("repo returns a failure while getting namespaces", func(t *testing.T) {
		// given
		tf.NewTestFixture(t, s.DB, tf.Tenants(1, func(fxt *tf.TestFixture, idx int) error {
			fxt.Tenants[idx].NsBaseName = "failingJohny"
			return nil
		}))
		svc := &serviceWithFailures{
			Service:      tenant.NewDBService(s.DB),
			errsToReturn: &[]error{nil, nil, gorm.ErrInvalidSQL},
		}
		// when
		_, err := tenant.ConstructNsBaseName(svc, "failingJohny")
		// then
		test.AssertError(t, err,
			test.HasMessageContaining("getting already existing namespaces with the name failingJohny2-che failed"),
			test.IsOfType(gorm.ErrInvalidSQL))
	})
}

type serviceWithFailures struct {
	tenant.Service
	errsToReturn *[]error
}

func (s serviceWithFailures) ExistsWithNsBaseName(nsUsername string) (bool, error) {
	if len(*s.errsToReturn) > 0 {
		errToReturn := (*s.errsToReturn)[0]
		*s.errsToReturn = (*s.errsToReturn)[1:]
		if errToReturn != nil {
			return false, errToReturn
		}
	}
	return s.Service.ExistsWithNsBaseName(nsUsername)
}

func (s serviceWithFailures) NamespaceExists(nsName string) (bool, error) {
	if len(*s.errsToReturn) > 0 {
		errToReturn := (*s.errsToReturn)[0]
		*s.errsToReturn = (*s.errsToReturn)[1:]
		if errToReturn != nil {
			return false, errToReturn
		}
	}
	return s.Service.NamespaceExists(nsName)
}
