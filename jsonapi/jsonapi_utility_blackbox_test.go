package jsonapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/fabric8-services/fabric8-tenant/app"
	"github.com/fabric8-services/fabric8-tenant/errors"
	"github.com/fabric8-services/fabric8-tenant/jsonapi"
	"github.com/fabric8-services/fabric8-wit/resource"
	errs "github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestErrorToJSONAPIError(t *testing.T) {
	t.Parallel()
	resource.Require(t, resource.UnitTest)

	var jerr app.JSONAPIError
	var httpStatus int

	// test not found error
	jerr, httpStatus = jsonapi.ErrorToJSONAPIError(nil, errors.NewTenantRecordNotFoundError("foo", "bar"))
	require.Equal(t, http.StatusNotFound, httpStatus)
	require.NotNil(t, jerr.Code)
	require.NotNil(t, jerr.Status)
	require.Equal(t, jsonapi.ErrorCodeNotFound, *jerr.Code)
	require.Equal(t, strconv.Itoa(httpStatus), *jerr.Status)

	// test bad parameter error
	jerr, httpStatus = jsonapi.ErrorToJSONAPIError(nil, errors.NewBadParameterError("foo", "bar"))
	require.Equal(t, http.StatusBadRequest, httpStatus)
	require.NotNil(t, jerr.Code)
	require.NotNil(t, jerr.Status)
	require.Equal(t, jsonapi.ErrorCodeBadParameter, *jerr.Code)
	require.Equal(t, strconv.Itoa(httpStatus), *jerr.Status)

	// test internal server error
	jerr, httpStatus = jsonapi.ErrorToJSONAPIError(nil, errors.NewInternalError(context.Background(), errs.New("foo")))
	require.Equal(t, http.StatusInternalServerError, httpStatus)
	require.NotNil(t, jerr.Code)
	require.NotNil(t, jerr.Status)
	require.Equal(t, jsonapi.ErrorCodeInternalError, *jerr.Code)
	require.Equal(t, strconv.Itoa(httpStatus), *jerr.Status)

	// test unauthorized error
	jerr, httpStatus = jsonapi.ErrorToJSONAPIError(nil, errors.NewUnauthorizedError("foo"))
	require.Equal(t, http.StatusUnauthorized, httpStatus)
	require.NotNil(t, jerr.Code)
	require.NotNil(t, jerr.Status)
	require.Equal(t, jsonapi.ErrorCodeUnauthorizedError, *jerr.Code)
	require.Equal(t, strconv.Itoa(httpStatus), *jerr.Status)

	// test namespace conflict error
	jerr, httpStatus = jsonapi.ErrorToJSONAPIError(nil, errors.NewNamespaceConflictError("foo"))
	require.Equal(t, http.StatusConflict, httpStatus)
	require.NotNil(t, jerr.Code)
	require.NotNil(t, jerr.Status)
	require.Equal(t, jsonapi.ErrorCodeNamespaceConflict, *jerr.Code)
	require.Equal(t, strconv.Itoa(httpStatus), *jerr.Status)

	// test forbidden error
	jerr, httpStatus = jsonapi.ErrorToJSONAPIError(nil, errors.NewForbiddenError("foo"))
	require.Equal(t, http.StatusForbidden, httpStatus)
	require.NotNil(t, jerr.Code)
	require.NotNil(t, jerr.Status)
	require.Equal(t, jsonapi.ErrorCodeForbiddenError, *jerr.Code)
	require.Equal(t, strconv.Itoa(httpStatus), *jerr.Status)

	// test quota exceeded error
	jerr, httpStatus = jsonapi.ErrorToJSONAPIError(nil, errors.NewQuotaExceedError("foo"))
	require.Equal(t, http.StatusForbidden, httpStatus)
	require.NotNil(t, jerr.Code)
	require.NotNil(t, jerr.Status)
	require.Equal(t, jsonapi.ErrorCodeQuotaExceedError, *jerr.Code)
	require.Equal(t, strconv.Itoa(httpStatus), *jerr.Status)

	// test unspecified error
	jerr, httpStatus = jsonapi.ErrorToJSONAPIError(nil, fmt.Errorf("foobar"))
	require.Equal(t, http.StatusInternalServerError, httpStatus)
	require.NotNil(t, jerr.Code)
	require.NotNil(t, jerr.Status)
	require.Equal(t, jsonapi.ErrorCodeUnknownError, *jerr.Code)
	require.Equal(t, strconv.Itoa(httpStatus), *jerr.Status)
}
