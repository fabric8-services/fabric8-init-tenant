package configuration

import (
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/almighty/almighty-core/resource"
	"github.com/goadesign/goa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var reqLong *goa.RequestData
var reqShort *goa.RequestData
var config *Data

func init() {

	// ensure that the content here is executed only once.
	reqLong = &goa.RequestData{
		Request: &http.Request{Host: "api.service.domain.org"},
	}
	reqShort = &goa.RequestData{
		Request: &http.Request{Host: "api.domain.org"},
	}
	resetConfiguration()
}

func resetConfiguration() {
	var err error
	config, err = NewData()
	if err != nil {
		panic(fmt.Errorf("Failed to setup the configuration: %s", err.Error()))
	}
}

func TestOpenIDConnectPathOK(t *testing.T) {
	resource.Require(t, resource.UnitTest)
	t.Parallel()

	path := config.openIDConnectPath("somesufix")
	assert.Equal(t, "auth/realms/"+config.GetKeycloakRealm()+"/protocol/openid-connect/somesufix", path)
}

func TestGetKeycloakURLOK(t *testing.T) {
	resource.Require(t, resource.UnitTest)
	t.Parallel()

	url, err := config.getKeycloakURL(reqLong, "somepath")
	assert.Nil(t, err)
	assert.Equal(t, "http://sso.service.domain.org/somepath", url)

	url, err = config.getKeycloakURL(reqShort, "somepath2")
	assert.Nil(t, err)
	assert.Equal(t, "http://sso.domain.org/somepath2", url)
}

func TestGetKeycloakHttpsURLOK(t *testing.T) {
	resource.Require(t, resource.UnitTest)
	t.Parallel()

	r, err := http.NewRequest("", "https://sso.domain.org", nil)
	require.Nil(t, err)
	req := &goa.RequestData{
		Request: r,
	}

	url, err := config.getKeycloakURL(req, "somepath")
	assert.Nil(t, err)
	assert.Equal(t, "https://sso.domain.org/somepath", url)
}

func TestGetKeycloakURLForTooShortHostFails(t *testing.T) {
	resource.Require(t, resource.UnitTest)
	t.Parallel()

	r := &goa.RequestData{
		Request: &http.Request{Host: "org"},
	}
	_, err := config.getKeycloakURL(r, "somepath")
	assert.NotNil(t, err)
}

func TestKeycloakRealmInDevModeCanBeOverridden(t *testing.T) {
	resource.Require(t, resource.UnitTest)

	key := "F8_KEYCLOAK_REALM"
	realEnvValue := os.Getenv(key)

	os.Unsetenv(key)
	defer func() {
		os.Setenv(key, realEnvValue)
		resetConfiguration()
	}()

	assert.Equal(t, devModeKeycloakRealm, config.GetKeycloakRealm())

	os.Setenv(key, "somecustomrealm")
	resetConfiguration()

	assert.Equal(t, "somecustomrealm", config.GetKeycloakRealm())
}
