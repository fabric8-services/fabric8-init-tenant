package keycloak_test

import (
	"testing"

	"github.com/fabric8-services/fabric8-tenant/keycloak"
	"github.com/stretchr/testify/assert"
)

func TestPublicKeys(t *testing.T) {

	t.Run("valid keys", func(t *testing.T) {
		u, err := keycloak.GetPublicKeys("https://auth.prod-preview.openshift.io")
		assert.NoError(t, err)
		assert.Equal(t, 2, len(u))
	})
	t.Run("invalid url", func(t *testing.T) {
		_, err := keycloak.GetPublicKeys("http://google.com")
		assert.Error(t, err)
	})
}
