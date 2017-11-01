package keycloak

import (
	"context"

	jwt "github.com/dgrijalva/jwt-go"
	goajwt "github.com/goadesign/goa/middleware/security/jwt"
)

// IsSpecificServiceAccount checks if the request is done by a given
// Service account based on the JWT Token provided in context
func IsSpecificServiceAccount(ctx context.Context, name string) bool {
	accountname, ok := extractServiceAccountName(ctx)
	if !ok {
		return false
	}
	return accountname == name
}

// IsServiceAccount checks if the request is done by a
// Service account based on the JWT Token provided in context
func IsServiceAccount(ctx context.Context) bool {
	_, ok := extractServiceAccountName(ctx)
	return ok
}

func extractServiceAccountName(ctx context.Context) (string, bool) {
	token := goajwt.ContextJWT(ctx)
	if token == nil {
		return "", false
	}
	accountName := token.Claims.(jwt.MapClaims)["service_accountname"]
	if accountName == nil {
		return "", false
	}
	accountNameTyped, isString := accountName.(string)
	return accountNameTyped, isString
}
