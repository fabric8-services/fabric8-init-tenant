package openshift_test

import (
	"context"
	"fmt"
	"github.com/fabric8-services/fabric8-common/convert/ptr"
	"github.com/fabric8-services/fabric8-tenant/cluster"
	"github.com/fabric8-services/fabric8-tenant/configuration"
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

type ActionTestSuite struct {
	gormsupport.DBTestSuite
}

func TestAction(t *testing.T) {
	suite.Run(t, &ActionTestSuite{DBTestSuite: gormsupport.NewDBTestSuite("../config.yaml")})
}

var emptyHealing = openshift.NoHealing(nil)
var returnErrHealing openshift.Healing = func(originalError error) error {
	return fmt.Errorf("healing error")
}
var myDummyRole = openshift.NewObject(environment.ValKindRole, "mynamespace", "myname")

func (s *ActionTestSuite) TestCreateAction() {
	// given
	fxt := tf.FillDB(s.T(), s.DB, tf.AddTenants(1), tf.AddNamespaces())
	id := fxt.Tenants[0].ID
	repoService := tenant.NewDBService(s.DB)
	repo := repoService.NewTenantRepository(id)
	config, reset := test.LoadTestConfig(s.T())
	defer reset()

	// when
	create := openshift.NewCreateAction(repo, openshift.CreateOpts().EnableSelfHealing())

	// then
	s.T().Run("method name should match", func(t *testing.T) {
		assert.Equal(t, "POST", create.MethodName())
	})

	s.T().Run("filter method should always return true", func(t *testing.T) {
		for _, obj := range getObjectsOfAllKinds() {
			assert.True(t, create.Filter()(obj))
		}
	})

	s.T().Run("GetOperationSets should not add additional object and should sort the objects", func(t *testing.T) {
		// when
		_, sets, err := create.GetOperationSets(NewAllTypesService(nil, true), openshift.Client{})
		// then
		assert.NoError(t, err)
		require.Len(t, sets, 1)
		assert.Equal(t, "POST", sets[0].Method)
		postSorted := sets[0].Objects
		require.Len(t, postSorted, len(allKinds))
		assert.Equal(t, environment.ValKindProjectRequest, environment.GetKind(postSorted[0]))
		assert.Equal(t, environment.ValKindRole, environment.GetKind(postSorted[1]))
		assert.Equal(t, environment.ValKindRoleBindingRestriction, environment.GetKind(postSorted[2]))
	})

	s.T().Run("GetOperationSets should add additional object to existing set and sort the objects", func(t *testing.T) {
		// when
		_, sets, err := create.GetOperationSets(NewAllTypesService(myDummyRole, true), openshift.Client{})
		// then
		assert.NoError(t, err)
		require.Len(t, sets, 1)
		assert.Equal(t, "POST", sets[0].Method)
		postSorted := sets[0].Objects
		require.Len(t, postSorted, len(allKinds)+1)
		assert.Equal(t, environment.ValKindProjectRequest, environment.GetKind(postSorted[0]))
		assert.Equal(t, environment.ValKindRole, environment.GetKind(postSorted[1]))
		assert.Equal(t, environment.ValKindRole, environment.GetKind(postSorted[2]))
		assert.Equal(t, environment.ValKindRoleBindingRestriction, environment.GetKind(postSorted[3]))
	})

	s.T().Run("GetOperationSets should add additional object to new set and sort the objects", func(t *testing.T) {
		// when
		_, sets, err := create.GetOperationSets(NewAllTypesService(myDummyRole, false), openshift.Client{})
		// then
		assert.NoError(t, err)
		require.Len(t, sets, 2)
		assert.Equal(t, "POST", sets[0].Method)
		postSorted := sets[0].Objects
		require.Len(t, postSorted, len(allKinds))
		assert.Equal(t, environment.ValKindProjectRequest, environment.GetKind(postSorted[0]))
		assert.Equal(t, environment.ValKindRole, environment.GetKind(postSorted[1]))
		assert.Equal(t, environment.ValKindRoleBindingRestriction, environment.GetKind(postSorted[2]))

		assert.Equal(t, "DELETE", sets[1].Method)
		deleteSorted := sets[1].Objects
		require.Len(t, deleteSorted, 1)
		assert.Equal(t, myDummyRole, deleteSorted[0])
	})

	s.T().Run("it should not require master token globally", func(t *testing.T) {
		assert.False(t, create.ForceMasterTokenGlobally())
	})

	for idx, envType := range environment.DefaultEnvTypes {
		// given
		envService, envData := gewEnvServiceWithData(s.T(), envType, config)
		// when
		namespace, err := create.GetNamespaceEntity(envService)
		// then
		assert.NoError(s.T(), err)
		s.T().Run("verify new namespace was created", func(t *testing.T) {
			assertion.AssertTenant(t, repo).
				HasNumberOfNamespaces(idx + 1).
				HasNamespaceOfTypeThat(envType).
				HasState(tenant.Provisioning).
				HasMasterURL(test.ClusterURL)
		})

		s.T().Run("update namespace to ready", func(t *testing.T) {
			// when
			create.UpdateNamespace(envData, &cluster.Cluster{APIURL: test.ClusterURL}, namespace, false)
			// then
			assertion.AssertTenant(t, repo).
				HasNamespaceOfTypeThat(envType).
				HasState(tenant.Ready).
				HasMasterURL(test.ClusterURL).
				HasNameWithBaseName("developer1")
		})

		s.T().Run("update namespace to failed", func(t *testing.T) {
			// when
			create.UpdateNamespace(envData, &cluster.Cluster{APIURL: test.ClusterURL}, namespace, true)
			// then
			assertion.AssertTenant(t, repo).
				HasNamespaceOfTypeThat(envType).
				HasState(tenant.Failed).
				HasMasterURL(test.ClusterURL).
				HasNameWithBaseName("developer1")
		})
	}

	s.T().Run("ManageAndUpdateResults should do nothing when err channel is empty", func(t *testing.T) {
		// given
		errorChan := make(chan error, 10)
		close(errorChan)
		// when
		assert.NoError(t, create.ManageAndUpdateResults(errorChan, []environment.Type{environment.TypeChe, environment.TypeUser}, emptyHealing))
		// then
		assertion.AssertTenant(t, repo).Exists()
	})

	s.T().Run("ManageAndUpdateResults should return error when err channel is not empty and healing is empty", func(t *testing.T) {
		// given
		errorChan := make(chan error, 10)
		errorChan <- fmt.Errorf("first dummy error")
		errorChan <- fmt.Errorf("second dummy error")
		close(errorChan)
		// when
		err := create.ManageAndUpdateResults(errorChan, []environment.Type{environment.TypeChe, environment.TypeUser}, emptyHealing)
		// then
		test.AssertError(t, err, test.HasMessageContaining("POST method applied to namespace types [che user] failed with one or more errors"),
			test.HasMessageContaining("#1: first dummy error"),
			test.HasMessageContaining("#2: second dummy error"))
	})

	s.T().Run("HealingStrategy should return healing strategy that re-creates new namespaces (with new base name) when error is not nil", func(t *testing.T) {

		t.Run("when there was an error, then should delete and create with basename developer2", func(t *testing.T) {
			// given
			fxt := tf.FillDB(t, s.DB, tf.AddSpecificTenants(tf.SingleWithName("developer")), tf.AddNamespaces(environment.TypeUser, environment.TypeChe))
			id := fxt.Tenants[0].ID
			fmt.Println(id)
			repoService := tenant.NewDBService(s.DB)
			repo := repoService.NewTenantRepository(id)

			defer gock.OffAll()
			deleteCalls := 0
			postCalls := 0
			testdoubles.MockPostRequestsToOS(&postCalls, test.ClusterURL, environment.DefaultEnvTypes, "developer2")
			testdoubles.MockRemoveRequestsToOS(&deleteCalls, test.ClusterURL)
			userModifier := testdoubles.AddUser("developer")
			serviceBuilder := testdoubles.NewOSService(config, userModifier, repo)
			// when
			err := openshift.NewCreateAction(repo, openshift.CreateOpts().EnableSelfHealing()).
				HealingStrategy()(serviceBuilder)(fmt.Errorf("some error"))
			// then
			assert.NoError(t, err)
			assert.EqualValues(t, testdoubles.ExpectedNumberOfCallsWhenPost(t, config), postCalls)
			assert.EqualValues(t, 2, deleteCalls)
			assertion.AssertTenant(t, repo).HasNsBaseName("developer2")
		})

		t.Run("when there was an error and dev2 already exists then it should create dev3", func(t *testing.T) {
			// given
			fxt := tf.FillDB(t, s.DB, tf.AddSpecificTenants(tf.SingleWithName("dev"), tf.SingleWithName("dev2")),
				tf.AddNamespaces(environment.TypeUser, environment.TypeChe))
			id := fxt.Tenants[0].ID
			fmt.Println(id)
			repoService := tenant.NewDBService(s.DB)
			repo := repoService.NewTenantRepository(id)

			defer gock.OffAll()
			deleteCalls := 0
			postCalls := 0
			testdoubles.MockPostRequestsToOS(&postCalls, test.ClusterURL, environment.DefaultEnvTypes, "dev3")
			testdoubles.MockRemoveRequestsToOS(&deleteCalls, test.ClusterURL)
			userModifier := testdoubles.AddUser("dev")
			serviceBuilder := testdoubles.NewOSService(config, userModifier, repo)
			// when
			err := openshift.NewCreateAction(repo, openshift.CreateOpts().EnableSelfHealing()).
				HealingStrategy()(serviceBuilder)(fmt.Errorf("some error"))
			// then
			assert.NoError(t, err)
			assert.EqualValues(t, testdoubles.ExpectedNumberOfCallsWhenPost(t, config), postCalls)
			assert.EqualValues(t, 2, deleteCalls)
			assertion.AssertTenant(t, repo).HasNsBaseName("dev3")
		})

		t.Run("when deletion fails then it should stop recreation and return an error", func(t *testing.T) {
			// given
			fxt := tf.FillDB(t, s.DB, tf.AddSpecificTenants(tf.SingleWithName("developertofail")), tf.AddNamespaces(environment.TypeUser, environment.TypeChe))
			id := fxt.Tenants[0].ID
			repoService := tenant.NewDBService(s.DB)
			repo := repoService.NewTenantRepository(id)

			defer gock.OffAll()
			deleteCalls := 0
			gock.New(test.ClusterURL).
				Delete(".*/developertofail-che.*").
				Times(2).
				Reply(500).
				BodyString("{}")
			testdoubles.MockRemoveRequestsToOS(&deleteCalls, test.ClusterURL)
			userModifier := testdoubles.AddUser("developertofail")
			serviceBuilder := testdoubles.NewOSService(config, userModifier, repo)
			// when
			err := openshift.NewCreateAction(repo, openshift.CreateOpts().EnableSelfHealing()).
				HealingStrategy()(serviceBuilder)(fmt.Errorf("some error"))
			// then
			test.AssertError(t, err,
				test.HasMessageContaining("DELETE method applied to namespace types [che user] failed"),
				test.HasMessageContaining("server responded with status: 500 for the DELETE request"),
				test.HasMessageContaining("while doing self-healing operations triggered by error: [some error]"))
			assert.EqualValues(t, 1, deleteCalls)
			assertion.AssertTenant(t, repo).HasNsBaseName("developertofail")
		})

		t.Run("when recreation fails then it should not do another one and return an error", func(t *testing.T) {
			// given
			fxt := tf.FillDB(t, s.DB, tf.AddSpecificTenants(tf.SingleWithName("anotherdev")), tf.AddNamespaces(environment.TypeUser, environment.TypeChe))
			id := fxt.Tenants[0].ID
			repoService := tenant.NewDBService(s.DB)
			repo := repoService.NewTenantRepository(id)

			defer gock.OffAll()
			deleteCalls := 0
			postCalls := 0
			gock.New(test.ClusterURL).
				Post(".*/anotherdev/.*").
				Reply(500).
				BodyString("{}")
			gock.New(test.ClusterURL).
				Post(".*/anotherdev2/.*").
				Reply(500).
				BodyString("{}")
			testdoubles.MockPostRequestsToOS(&postCalls, test.ClusterURL, environment.DefaultEnvTypes, "anotherdev2")
			testdoubles.MockRemoveRequestsToOS(&deleteCalls, test.ClusterURL)
			userModifier := testdoubles.AddUser("anotherdev")
			serviceBuilder := testdoubles.NewOSService(config, userModifier, repo)
			// when
			err := openshift.NewCreateAction(repo, openshift.CreateOpts().EnableSelfHealing()).
				HealingStrategy()(serviceBuilder)(fmt.Errorf("some error"))
			// then
			test.AssertError(t, err,
				test.HasMessageContaining("POST method applied to namespace types [che user] failed"),
				test.HasMessageContaining("server responded with status: 500 for the POST request"),
				test.HasMessageContaining("while doing self-healing operations triggered by error: [some error]"))
			assert.EqualValues(t, 2, deleteCalls)
			assertion.AssertTenant(t, repo).HasNsBaseName("anotherdev2")
		})

		t.Run("healing should not be executed when disabled", func(t *testing.T) {
			// given
			errorChan := make(chan error, 10)
			errorChan <- fmt.Errorf("first dummy error")
			close(errorChan)
			// when
			err := openshift.NewCreateAction(repo, openshift.CreateOpts().DisableSelfHealing()).
				ManageAndUpdateResults(errorChan, []environment.Type{environment.TypeChe}, returnErrHealing)
			// then
			test.AssertError(t, err, test.HasMessageContaining("first dummy error"))
		})

		t.Run("when there was no error then it should not run healing", func(t *testing.T) {
			// given
			errorChan := make(chan error, 10)
			close(errorChan)
			// when
			err := openshift.NewCreateAction(repo, openshift.CreateOpts().EnableSelfHealing()).
				ManageAndUpdateResults(errorChan, []environment.Type{environment.TypeChe}, returnErrHealing)
			// then
			assert.NoError(t, err)
		})
	})
}

func (s *ActionTestSuite) TestDeleteAction() {
	// given
	fxt := tf.FillDB(s.T(), s.DB, tf.AddTenants(1), tf.AddNamespaces(environment.TypeUser, environment.TypeChe))
	id := fxt.Tenants[0].ID
	repoService := tenant.NewDBService(s.DB)
	repo := repoService.NewTenantRepository(id)
	config, reset := test.LoadTestConfig(s.T())
	defer reset()
	client := openshift.NewClient(nil, test.ClusterURL, tokenProducer)

	// when
	delete := openshift.NewDeleteAction(repo, fxt.Namespaces, openshift.DeleteOpts().EnableSelfHealing())
	deleteFromCluster := openshift.NewDeleteAction(repo, fxt.Namespaces, openshift.DeleteOpts().EnableSelfHealing().RemoveFromCluster())

	// then
	s.T().Run("method name should match", func(t *testing.T) {
		assert.Equal(t, "DELETE", delete.MethodName())
	})

	s.T().Run("verify filter method", func(t *testing.T) {
		for _, obj := range getObjectsOfAllKinds() {
			if environment.GetKind(obj) == "ProjectRequest" {
				assert.False(t, delete.Filter()(obj), obj.ToString())
				assert.True(t, deleteFromCluster.Filter()(obj), obj.ToString())
			} else {
				assert.False(t, deleteFromCluster.Filter()(obj), obj.ToString())
				if environment.GetKind(obj) == "PersistentVolumeClaim" || environment.GetKind(obj) == "ConfigMap" ||
					environment.GetKind(obj) == "Service" || environment.GetKind(obj) == "DeploymentConfig" || environment.GetKind(obj) == "Route" ||
					environment.GetKind(obj) == "Job" || environment.GetKind(obj) == "Deployment" {
					assert.True(t, delete.Filter()(obj), obj.ToString())
				} else {
					assert.False(t, delete.Filter()(obj), obj.ToString())
				}
			}
		}
	})

	s.T().Run("GetOperationSets method should do reverse sorted and delete all objects for clean", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New(test.ClusterURL).
			Get("/.+/namespaces/johny/[^/]+/$").
			Times(len(openshift.AllToGetAndDelete)).
			Reply(200).
			BodyString(`{"items": [{"metadata": {"name": "some-item"}}]}`)
		toSort := getObjectsOfAllKinds()
		// when
		_, sets, err := delete.GetOperationSets(NewAllTypesService(nil, false), *client)
		// then
		assert.NoError(t, err)
		assert.Len(t, sets, 2)
		length := len(toSort)
		deleteSet := sets[0]
		assert.Equal(t, "DELETE", deleteSet.Method)
		assert.Equal(t, environment.ValKindService, environment.GetKind(deleteSet.Objects[length-1]))
		assert.Equal(t, environment.ValKindPod, environment.GetKind(deleteSet.Objects[length-2]))
		assert.Equal(t, environment.ValKindPersistentVolumeClaim, environment.GetKind(deleteSet.Objects[length-3]))
		kindOrder := []string{environment.ValKindDeploymentConfig, environment.ValKindReplicationController, environment.ValKindPod}
		for i := 0; i < len(deleteSet.Objects) && len(kindOrder) > 0; i++ {
			if kindOrder[0] == environment.GetKind(deleteSet.Objects[i]) {
				kindOrder = kindOrder[1:]
			}
		}
		assert.Empty(t, kindOrder, "objects are not in correct order")
		assert.Equal(t, openshift.EnsureDeletion, sets[1].Method)
		assert.Equal(t, deleteSet.Objects, sets[1].Objects)
	})

	s.T().Run("GetOperationSets method should not fail when get returns 404 or 403", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New(test.ClusterURL).
			Get("/.+/namespaces/johny/[^/]+/$").
			Times(len(openshift.AllToGetAndDelete) / 2).
			Reply(404)
		gock.New(test.ClusterURL).
			Get("/.+/namespaces/johny/[^/]+/$").
			Times(len(openshift.AllToGetAndDelete)/2 + 1).
			Reply(403)
		// when
		_, sets, err := delete.GetOperationSets(NewAllTypesService(nil, false), *client)
		// then
		assert.NoError(t, err)
		assert.Len(t, sets, 2)
	})

	s.T().Run("GetOperationSets method should fail when get returns 505", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New(test.ClusterURL).
			Get("/.+/namespaces/johny/[^/]+/$").
			Reply(505)
		// when
		_, _, err := delete.GetOperationSets(NewAllTypesService(nil, false), *client)
		// then
		test.AssertError(t, err,
			test.HasMessageContaining("unable to get list of current objects of kind"),
			test.HasMessageContaining("server responded with status: 505 for the GET request"))
	})

	s.T().Run("GetOperationSets method should do reverse sorted and retrieve and parse services", func(t *testing.T) {
		// given
		defer gock.OffAll()
		gock.New(test.ClusterURL).
			Get("/api/v1/namespaces/johny/services").
			Reply(200).
			BodyString(`{"items": [
        {"metadata": {"name": "bayesian-link"}},
        {"metadata": {"name": "user"}},
        {"metadata": {"name": "cool-jnlp"}}]}`)
		gock.New(test.ClusterURL).
			Get("/.+/namespaces/johny/[^/]+/$").
			Times(len(openshift.AllToGetAndDelete)).
			Reply(200).
			BodyString(`{"items": []}`)
		allTypesService := NewAllTypesService(nil, false)
		allTypesService.allObjects = []environment.Object{}
		// when
		_, sets, err := delete.GetOperationSets(allTypesService, *client)
		// then
		assert.NoError(t, err)
		assert.Len(t, sets, 2)
		deleteSet := sets[0]
		assert.Equal(t, "DELETE", deleteSet.Method)
		require.Len(t, deleteSet.Objects, 3)
		assert.Equal(t, environment.ValKindService, environment.GetKind(deleteSet.Objects[0]))
		assert.Equal(t, "bayesian-link", environment.GetName(deleteSet.Objects[0]))
		assert.Equal(t, environment.ValKindService, environment.GetKind(deleteSet.Objects[1]))
		assert.Equal(t, "user", environment.GetName(deleteSet.Objects[1]))
		assert.Equal(t, environment.ValKindService, environment.GetKind(deleteSet.Objects[2]))
		assert.Equal(t, "cool-jnlp", environment.GetName(deleteSet.Objects[2]))

		assert.Equal(t, openshift.EnsureDeletion, sets[1].Method)
		assert.Equal(t, deleteSet.Objects, sets[1].Objects)
	})

	s.T().Run("GetOperationSets method should do reverse sorted and and not delete all objects for remove", func(t *testing.T) {
		// when
		_, sets, err := deleteFromCluster.GetOperationSets(NewAllTypesService(nil, false), *client)
		// then
		assert.NoError(t, err)
		assert.Len(t, sets, 1)
		length := len(allKinds)
		assert.Equal(t, "DELETE", sets[0].Method)
		sorted := sets[0].Objects
		require.Len(t, sorted, length)
		assert.Equal(t, environment.ValKindProjectRequest, environment.GetKind(sorted[length-1]))
		assert.Equal(t, environment.ValKindRole, environment.GetKind(sorted[length-2]))
		assert.Equal(t, environment.ValKindRoleBindingRestriction, environment.GetKind(sorted[length-3]))
	})

	s.T().Run("it should require master token globally", func(t *testing.T) {
		assert.True(t, delete.ForceMasterTokenGlobally())
	})

	for _, envType := range environment.DefaultEnvTypes {
		// given
		envService, envData := gewEnvServiceWithData(s.T(), envType, config)

		// verify getting namespace - it should return only if exists
		namespace, err := delete.GetNamespaceEntity(envService)
		assert.NoError(s.T(), err)

		s.T().Run("verify new namespace is returned only if exists", func(t *testing.T) {
			if envType == environment.TypeChe || envType == environment.TypeUser {
				assertion.AssertNamespace(t, namespace).
					IsOFType(envType).
					HasState(tenant.Ready)
			} else {
				assert.Nil(t, namespace)
			}
		})
		if namespace == nil {
			continue
		}

		s.T().Run("update namespace does nothing when ns is only cleaned", func(t *testing.T) {
			// when
			delete.UpdateNamespace(envData, &cluster.Cluster{APIURL: test.ClusterURL}, namespace, false)
			// then
			assertion.AssertTenant(t, repo).
				HasNamespaceOfTypeThat(envType).
				HasState(tenant.Ready).
				HasMasterURL(test.ClusterURL)
		})

		s.T().Run("update namespace set state to failed", func(t *testing.T) {
			// when
			delete.UpdateNamespace(envData, &cluster.Cluster{APIURL: test.ClusterURL}, namespace, true)
			// then
			assertion.AssertTenant(t, repo).
				HasNamespaceOfTypeThat(envType).
				HasState(tenant.Failed)
		})

		s.T().Run("update namespace deletes entity when it should be removed from cluster", func(t *testing.T) {
			// when
			deleteFromCluster.UpdateNamespace(envData, &cluster.Cluster{APIURL: test.ClusterURL}, namespace, false)
			// then
			assertion.AssertTenant(t, repo).HasNotNamespaceOfType(envType)
		})
	}

	s.T().Run("ManageAndUpdateResults should keep entity when one namespace is present", func(t *testing.T) {
		// given
		tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(func(tnnt *tenant.Tenant) {
			tnnt.ID = id
		}), tf.AddNamespaces(environment.TypeUser, environment.TypeChe))

		errorChan := make(chan error, 10)
		close(errorChan)
		// when
		err := deleteFromCluster.ManageAndUpdateResults(errorChan, []environment.Type{environment.TypeChe}, emptyHealing)
		// then
		test.AssertError(t, err, test.HasMessageContaining("cannot remove tenant %s from DB - some namespaces", id))

	})

	s.T().Run("ManageAndUpdateResults should do nothing when namespace were only cleaned", func(t *testing.T) {
		// given
		tf.FillDB(s.T(), s.DB, tf.AddSpecificTenants(func(tnnt *tenant.Tenant) {
			tnnt.ID = id
		}), tf.AddNamespaces(environment.TypeUser, environment.TypeChe))
		errorChan := make(chan error, 10)
		close(errorChan)
		// when
		err := delete.ManageAndUpdateResults(errorChan, []environment.Type{environment.TypeChe}, emptyHealing)
		// then
		assert.NoError(t, err)

	})

	s.T().Run("ManageAndUpdateResults should delete entity when no namespace is present", func(t *testing.T) {
		// given
		repo := tenant.NewTenantRepository(s.DB, id)
		namespaces, err := repo.GetNamespaces()
		require.NoError(t, err)
		for _, ns := range namespaces {
			require.NoError(t, repo.DeleteNamespace(ns))
		}
		errorChan := make(chan error, 10)
		close(errorChan)
		// when
		err = deleteFromCluster.ManageAndUpdateResults(errorChan, []environment.Type{environment.TypeChe}, emptyHealing)
		// then
		assert.NoError(t, err)
		assertion.AssertTenant(t, repo).DoesNotExist()
	})

	s.T().Run("HealingStrategy should return healing strategy that re-does the delete when error is not nil", func(t *testing.T) {

		t.Run("when there was an error, then should redo clean and call delete calls another time", func(t *testing.T) {
			// given
			fxt := tf.FillDB(t, s.DB, tf.AddSpecificTenants(tf.SingleWithName("developer")), tf.AddNamespaces(environment.TypeUser, environment.TypeChe))
			id := fxt.Tenants[0].ID
			fmt.Println(id)
			repoService := tenant.NewDBService(s.DB)
			repo := repoService.NewTenantRepository(id)

			defer gock.OffAll()
			calls := 0
			testdoubles.MockCleanRequestsToOS(&calls, test.ClusterURL)
			userModifier := testdoubles.AddUser("developer")
			serviceBuilder := testdoubles.NewOSService(config, userModifier, repo)
			// when
			err := openshift.NewDeleteAction(repo, fxt.Namespaces, openshift.DeleteOpts().EnableSelfHealing()).
				HealingStrategy()(serviceBuilder)(fmt.Errorf("some error"))
			// then
			assert.NoError(t, err)
			expectedNumberOfCalls := testdoubles.ExpectedNumberOfCallsWhenClean(environment.TypeUser, environment.TypeChe)
			assert.EqualValues(t, expectedNumberOfCalls, calls)
			assert.NoError(t, err)
			assertion.AssertTenant(t, repo).
				HasNsBaseName("developer").
				HasNumberOfNamespaces(2)
		})

		t.Run("when there was an error, then should redo delete and call delete calls another time", func(t *testing.T) {
			// given
			fxt := tf.FillDB(t, s.DB, tf.AddSpecificTenants(tf.SingleWithName("developer")), tf.AddNamespaces(environment.TypeUser, environment.TypeChe))
			id := fxt.Tenants[0].ID
			fmt.Println(id)
			repoService := tenant.NewDBService(s.DB)
			repo := repoService.NewTenantRepository(id)

			defer gock.OffAll()
			calls := 0
			testdoubles.MockRemoveRequestsToOS(&calls, test.ClusterURL)
			userModifier := testdoubles.AddUser("developer")
			serviceBuilder := testdoubles.NewOSService(config, userModifier, repo)
			// when
			err := openshift.NewDeleteAction(repo, fxt.Namespaces, openshift.DeleteOpts().EnableSelfHealing().RemoveFromCluster()).
				HealingStrategy()(serviceBuilder)(fmt.Errorf("some error"))
			// then
			assert.NoError(t, err)
			assert.EqualValues(t, 2, calls)
			assert.NoError(t, err)
			assertion.AssertTenant(t, repo).DoesNotExist()
		})

		t.Run("when the second attempts for the clean fails, then it should stop trying again and return error", func(t *testing.T) {
			// given
			fxt := tf.FillDB(t, s.DB, tf.AddSpecificTenants(tf.SingleWithName("anotherdev")), tf.AddNamespaces(environment.TypeUser, environment.TypeChe))
			id := fxt.Tenants[0].ID
			fmt.Println(id)
			repoService := tenant.NewDBService(s.DB)
			repo := repoService.NewTenantRepository(id)

			defer gock.OffAll()
			gock.New(test.ClusterURL).
				Delete(".*/anotherdev/persistentvolumeclaims.*").
				Reply(500).
				BodyString("{}")
			testdoubles.MockCleanRequestsToOS(ptr.Int(0), test.ClusterURL)
			userModifier := testdoubles.AddUser("anotherdev")
			serviceBuilder := testdoubles.NewOSService(config, userModifier, repo)
			// when
			err := openshift.NewDeleteAction(repo, fxt.Namespaces, openshift.DeleteOpts().EnableSelfHealing()).
				HealingStrategy()(serviceBuilder)(fmt.Errorf("some error"))
			// then
			test.AssertError(t, err,
				test.HasMessageContaining("DELETE method applied to namespace types [che user] failed"),
				test.HasMessageContaining("server responded with status: 500 for the DELETE request"),
				test.HasMessageContaining("unable to redo the given action for the existing namespaces while doing"))
			assertion.AssertTenant(t, repo).
				HasNsBaseName("anotherdev").
				HasNumberOfNamespaces(2)
		})

		t.Run("when the second attempts for the delete fails, then it should stop trying again and return error", func(t *testing.T) {
			// given
			fxt := tf.FillDB(t, s.DB, tf.AddSpecificTenants(tf.SingleWithName("anotherdev")), tf.AddNamespaces(environment.TypeUser, environment.TypeChe))
			id := fxt.Tenants[0].ID
			fmt.Println(id)
			repoService := tenant.NewDBService(s.DB)
			repo := repoService.NewTenantRepository(id)

			defer gock.OffAll()
			gock.New(test.ClusterURL).
				Delete(".*/anotherdev").
				Reply(500).
				BodyString("{}")
			testdoubles.MockRemoveRequestsToOS(ptr.Int(0), test.ClusterURL)
			userModifier := testdoubles.AddUser("anotherdev")
			serviceBuilder := testdoubles.NewOSService(config, userModifier, repo)
			// when
			err := openshift.NewDeleteAction(repo, fxt.Namespaces, openshift.DeleteOpts().EnableSelfHealing().RemoveFromCluster()).
				HealingStrategy()(serviceBuilder)(fmt.Errorf("some error"))
			// then
			test.AssertError(t, err,
				test.HasMessageContaining("DELETE method applied to namespace types [che user] failed"),
				test.HasMessageContaining("server responded with status: 500 for the DELETE request"),
				test.HasMessageContaining("unable to redo the given action for the existing namespaces while doing"))
			assertion.AssertTenant(t, repo).HasNsBaseName("anotherdev")
		})

		t.Run("healing should not be executed when disabled for delete", func(t *testing.T) {
			// given
			errorChan := make(chan error, 10)
			errorChan <- fmt.Errorf("first dummy error")
			close(errorChan)
			// when
			err := openshift.NewDeleteAction(repo, fxt.Namespaces, openshift.DeleteOpts().DisableSelfHealing().RemoveFromCluster()).
				ManageAndUpdateResults(errorChan, []environment.Type{environment.TypeChe}, returnErrHealing)
			// then
			test.AssertError(t, err, test.HasMessageContaining("first dummy error"))
		})

		t.Run("healing should not be executed when disabled for clean", func(t *testing.T) {
			// given
			errorChan := make(chan error, 10)
			errorChan <- fmt.Errorf("first dummy error")
			close(errorChan)
			// and also when
			err := openshift.NewDeleteAction(repo, fxt.Namespaces, openshift.DeleteOpts().DisableSelfHealing()).
				ManageAndUpdateResults(errorChan, []environment.Type{environment.TypeChe}, returnErrHealing)
			// then
			test.AssertError(t, err, test.HasMessageContaining("first dummy error"))
		})

		s.T().Run("when there was no error then it should not run healing", func(t *testing.T) {
			// given
			errorChan := make(chan error, 10)
			close(errorChan)
			// when
			err := deleteFromCluster.ManageAndUpdateResults(errorChan, []environment.Type{environment.TypeChe}, returnErrHealing)
			// then
			assert.NoError(t, err)
			// and also when
			err = delete.ManageAndUpdateResults(errorChan, []environment.Type{environment.TypeChe}, returnErrHealing)
			// then
			assert.NoError(t, err)
		})
	})
}

func (s *ActionTestSuite) TestUpdateAction() {
	// given
	fxt := tf.FillDB(s.T(), s.DB, tf.AddTenants(1), tf.AddNamespaces(environment.TypeChe, environment.TypeUser).State(tenant.Updating))
	namespaces := fxt.Namespaces
	id := fxt.Tenants[0].ID

	repoService := tenant.NewDBService(s.DB)
	repo := repoService.NewTenantRepository(id)
	config, reset := test.LoadTestConfig(s.T())
	defer reset()

	// when
	update := openshift.NewUpdateAction(repo, namespaces, openshift.UpdateOpts().EnableSelfHealing())

	// then
	s.T().Run("method name should match", func(t *testing.T) {
		assert.Equal(t, "PATCH", update.MethodName())
	})

	s.T().Run("filter method should always return true except for project request", func(t *testing.T) {
		for _, obj := range getObjectsOfAllKinds() {
			if environment.GetKind(obj) == "ProjectRequest" {
				assert.False(t, update.Filter()(obj))
			} else {
				assert.True(t, update.Filter()(obj))
			}

		}
	})

	s.T().Run("GetOperationSets should not add additional set and should sort the objects", func(t *testing.T) {
		// when
		_, sets, err := update.GetOperationSets(NewAllTypesService(nil, true), openshift.Client{})
		// then
		assert.NoError(t, err)
		require.Len(t, sets, 1)
		assert.Equal(t, "PATCH", sets[0].Method)
		sorted := sets[0].Objects
		require.Len(t, sorted, len(allKinds))
		assert.Equal(t, environment.ValKindProjectRequest, environment.GetKind(sorted[0]))
		assert.Equal(t, environment.ValKindRole, environment.GetKind(sorted[1]))
		assert.Equal(t, environment.ValKindRoleBindingRestriction, environment.GetKind(sorted[2]))
	})

	s.T().Run("GetOperationSets should add additional object to existing set and sort the objects", func(t *testing.T) {
		// when
		_, sets, err := update.GetOperationSets(NewAllTypesService(myDummyRole, true), openshift.Client{})
		// then
		assert.NoError(t, err)
		require.Len(t, sets, 1)
		assert.Equal(t, "PATCH", sets[0].Method)
		patchSorted := sets[0].Objects
		require.Len(t, patchSorted, len(allKinds)+1)
		assert.Equal(t, environment.ValKindProjectRequest, environment.GetKind(patchSorted[0]))
		assert.Equal(t, environment.ValKindRole, environment.GetKind(patchSorted[1]))
		assert.Equal(t, environment.ValKindRole, environment.GetKind(patchSorted[2]))
		assert.Equal(t, environment.ValKindRoleBindingRestriction, environment.GetKind(patchSorted[3]))
	})

	s.T().Run("GetOperationSets should add additional object to new set and sort the objects", func(t *testing.T) {
		// when
		_, sets, err := update.GetOperationSets(NewAllTypesService(myDummyRole, false), openshift.Client{})
		// then
		assert.NoError(t, err)
		require.Len(t, sets, 2)
		assert.Equal(t, "PATCH", sets[0].Method)
		patchSorted := sets[0].Objects
		require.Len(t, patchSorted, len(allKinds))
		assert.Equal(t, environment.ValKindProjectRequest, environment.GetKind(patchSorted[0]))
		assert.Equal(t, environment.ValKindRole, environment.GetKind(patchSorted[1]))
		assert.Equal(t, environment.ValKindRoleBindingRestriction, environment.GetKind(patchSorted[2]))

		assert.Equal(t, "DELETE", sets[1].Method)
		deleteSorted := sets[1].Objects
		require.Len(t, deleteSorted, 1)
		assert.Equal(t, myDummyRole, deleteSorted[0])
	})

	s.T().Run("it should require master token globally", func(t *testing.T) {
		assert.True(t, update.ForceMasterTokenGlobally())
	})

	for _, envType := range environment.DefaultEnvTypes {
		// given
		envService, envData := gewEnvServiceWithData(s.T(), envType, config)

		// verify getting namespace - it should return only if exists
		namespace, err := update.GetNamespaceEntity(envService)
		assert.NoError(s.T(), err)

		s.T().Run("verify new namespace is returned only if exists", func(t *testing.T) {
			if envType == environment.TypeChe || envType == environment.TypeUser {
				assertion.AssertTenant(t, repo).
					HasNumberOfNamespaces(2).
					HasNamespaceOfTypeThat(envType).
					HasState(tenant.Updating)
			} else {
				assert.Nil(t, namespace)
			}
		})
		if namespace == nil {
			continue
		}

		// verify namespace update to ready
		s.T().Run("update namespace to ready", func(t *testing.T) {
			// when
			update.UpdateNamespace(envData, &cluster.Cluster{APIURL: test.ClusterURL}, namespace, false)
			// then
			assertion.AssertTenant(t, repo).
				HasNumberOfNamespaces(2).
				HasNamespaceOfTypeThat(envType).
				HasState(tenant.Ready).
				HasMasterURL(test.ClusterURL)
		})

		// verify namespace update to failed
		s.T().Run("update namespace to failed", func(t *testing.T) {
			// when
			update.UpdateNamespace(envData, &cluster.Cluster{APIURL: test.ClusterURL}, namespace, true)
			// then

			assertion.AssertTenant(t, repo).
				HasNumberOfNamespaces(2).
				HasNamespaceOfTypeThat(envType).
				HasState(tenant.Failed).
				HasMasterURL(test.ClusterURL)
		})
	}

	s.T().Run("ManageAndUpdateResults should do nothing when err channel is empty", func(t *testing.T) {
		// given
		errorChan := make(chan error, 10)
		close(errorChan)
		// when
		assert.NoError(t, update.ManageAndUpdateResults(errorChan, []environment.Type{environment.TypeChe, environment.TypeUser}, emptyHealing))
		// then
		assertion.AssertTenant(t, repo).Exists()
	})

	s.T().Run("ManageAndUpdateResults should return error when err channel is not empty", func(t *testing.T) {
		// given
		errorChan := make(chan error, 10)
		errorChan <- fmt.Errorf("first dummy error")
		errorChan <- fmt.Errorf("second dummy error")
		close(errorChan)
		// when
		err := update.ManageAndUpdateResults(errorChan, []environment.Type{environment.TypeChe, environment.TypeUser}, emptyHealing)
		// then
		test.AssertError(t, err,
			test.HasMessageContaining("PATCH method applied to namespace types [che user] failed with one or more errors"),
			test.HasMessageContaining("#1: first dummy error"),
			test.HasMessageContaining("#2: second dummy error"))
	})

	s.T().Run("HealingStrategy should return healing strategy that re-does the update when error is not nil", func(t *testing.T) {

		t.Run("when there was an error, then should redo update and call patch calls another time", func(t *testing.T) {
			// given
			fxt := tf.FillDB(t, s.DB, tf.AddSpecificTenants(tf.SingleWithName("developer")), tf.AddNamespaces(environment.TypeUser, environment.TypeChe))
			id := fxt.Tenants[0].ID
			fmt.Println(id)
			repoService := tenant.NewDBService(s.DB)
			repo := repoService.NewTenantRepository(id)

			defer gock.OffAll()
			calls := 0
			testdoubles.MockPatchRequestsToOS(&calls, test.ClusterURL)
			userModifier := testdoubles.AddUser("developer")
			serviceBuilder := testdoubles.NewOSService(config, userModifier, repo)
			// when
			err := openshift.NewUpdateAction(repo, namespaces, openshift.UpdateOpts().EnableSelfHealing()).
				HealingStrategy()(serviceBuilder)(fmt.Errorf("some error"))
			// then
			assert.NoError(t, err)
			expectedNumberOfCalls := testdoubles.ExpectedNumberOfCallsWhenPatch(t, s.Configuration, environment.TypeChe, environment.TypeUser)
			assert.EqualValues(t, expectedNumberOfCalls, calls)
			assert.NoError(t, err)
			assertion.AssertTenant(t, repo).
				HasNsBaseName("developer").
				HasNumberOfNamespaces(2)
		})

		t.Run("when the second attempts for the update fails, then it should stop trying again and return error", func(t *testing.T) {
			// given
			fxt := tf.FillDB(t, s.DB, tf.AddSpecificTenants(tf.SingleWithName("anotherdev")), tf.AddNamespaces(environment.TypeUser, environment.TypeChe))
			id := fxt.Tenants[0].ID
			fmt.Println(id)
			repoService := tenant.NewDBService(s.DB)
			repo := repoService.NewTenantRepository(id)

			defer gock.OffAll()
			gock.New(test.ClusterURL).
				Patch(".*/anotherdev/role.*").
				Reply(500).
				BodyString("{}")
			testdoubles.MockPatchRequestsToOS(ptr.Int(0), test.ClusterURL)
			userModifier := testdoubles.AddUser("anotherdev")
			serviceBuilder := testdoubles.NewOSService(config, userModifier, repo)
			// when
			err := openshift.NewUpdateAction(repo, namespaces, openshift.UpdateOpts().EnableSelfHealing()).
				HealingStrategy()(serviceBuilder)(fmt.Errorf("some error"))
			// then
			test.AssertError(t, err,
				test.HasMessageContaining("PATCH method applied to namespace types [che user] failed"),
				test.HasMessageContaining("server responded with status: 500 for the PATCH request"),
				test.HasMessageContaining("unable to redo the given action for the existing namespaces while doing"))
			assertion.AssertTenant(t, repo).
				HasNsBaseName("anotherdev").
				HasNumberOfNamespaces(2)
		})

		t.Run("healing should not be executed when disabled", func(t *testing.T) {
			// given
			errorChan := make(chan error, 10)
			errorChan <- fmt.Errorf("first dummy error")
			close(errorChan)
			// when
			err := openshift.NewUpdateAction(repo, namespaces, openshift.UpdateOpts().DisableSelfHealing()).
				ManageAndUpdateResults(errorChan, []environment.Type{environment.TypeChe}, returnErrHealing)
			// then
			test.AssertError(t, err, test.HasMessageContaining("first dummy error"))
		})

		t.Run("when there was no error then it should not run healing", func(t *testing.T) {
			// given
			errorChan := make(chan error, 10)
			close(errorChan)
			// when
			err := openshift.NewUpdateAction(repo, namespaces, openshift.UpdateOpts().EnableSelfHealing()).
				ManageAndUpdateResults(errorChan, []environment.Type{environment.TypeChe}, returnErrHealing)
			// then
			assert.NoError(t, err)
		})
	})
}

func gewEnvServiceWithData(t *testing.T, envType environment.Type, config *configuration.Data) (openshift.EnvironmentTypeService, *environment.EnvData) {
	osContext := openshift.NewServiceContext(
		context.Background(), config, testdoubles.DefaultClusterMapping, "developer", "developer1", func(cluster cluster.Cluster) string {
			return "HMs8laMmBSsJi8hpMDOtiglbXJ-2eyymE1X46ax5wX8"
		})
	service := openshift.NewEnvironmentTypeService(envType, osContext, environment.NewService())
	data, _, err := service.GetEnvDataAndObjects(func(objects environment.Object) bool {
		return true
	})
	assert.NoError(t, err)
	return service, data
}

type allTypesService struct {
	*openshift.CommonEnvTypeService
	allObjects       environment.Objects
	additionalObject environment.Object
	shouldBeAdded    bool
}

func NewAllTypesService(additionalObject environment.Object, shouldBeAdded bool) allTypesService {
	return allTypesService{
		allObjects:       getObjectsOfAllKinds(),
		additionalObject: additionalObject,
		shouldBeAdded:    shouldBeAdded,
	}
}

func (s allTypesService) GetEnvDataAndObjects(filter openshift.FilterFunc) (*environment.EnvData, environment.Objects, error) {
	return nil, s.allObjects, nil
}

func (s allTypesService) GetNamespaceName() string {
	return "johny"
}

func (s allTypesService) AdditionalObject() (environment.Object, bool) {
	return s.additionalObject, s.shouldBeAdded
}

var allKinds = []string{environment.ValKindPersistentVolumeClaim, environment.ValKindConfigMap,
	environment.ValKindLimitRange, environment.ValKindProject, environment.ValKindProjectRequest, environment.ValKindService,
	environment.ValKindSecret, environment.ValKindServiceAccount, environment.ValKindRoleBindingRestriction,
	environment.ValKindRoleBinding, environment.ValKindRole, environment.ValKindRoute, environment.ValKindJob,
	environment.ValKindList, environment.ValKindDeployment, environment.ValKindDeploymentConfig, environment.ValKindResourceQuota}

func getObjectsOfAllKinds() environment.Objects {
	var objects environment.Objects
	for _, kind := range allKinds {
		obj := map[interface{}]interface{}{"kind": kind}
		objects = append(objects, obj)
	}
	return objects
}
