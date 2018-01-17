package token

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/fabric8-services/fabric8-tenant/configuration"
)

func TestServiceAccountTokenClient_Get(t *testing.T) {

	want := "fake_token"
	output := `
		{
			"access_token": "` + want + `",
			"token_type": "bearer"
		}`
	tests := []struct {
		name    string
		wantErr bool
		URL     string
		status  int
		output  string
	}{
		{
			name:    "valid token response",
			wantErr: false,
		},
		{
			name:    "invalid URL given",
			wantErr: true,
			URL:     "google.com",
		},
		{
			name:    "fail on status code",
			wantErr: true,
			status:  http.StatusNotFound,
		},
		{
			name:    "make code fail on parsing output",
			wantErr: true,
			output:  "foobar",
		},
		{
			name:    "missing token from server",
			wantErr: true,
			output:  `{"token_type": "bearer"}`,
		},
		{
			name:    "empty token from server",
			wantErr: true,
			output:  `{"access_token": "","token_type": "bearer"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// if no status code given in test case, set the default
				if tt.status == 0 {
					tt.status = http.StatusOK
				}
				w.WriteHeader(tt.status)

				// if the output of the server is not set in testcase, set the default
				if tt.output == "" {
					tt.output = output
				}
				w.Write([]byte(tt.output))
			}))
			defer ts.Close()

			// if the URL is not given in test case then set what is given by user
			if tt.URL == "" {
				tt.URL = ts.URL
			}
			config, err := configuration.GetData()
			if err != nil {
				t.Fatalf("could not retrieve configuration: %v", err)
			}

			// set the URL given by the temporary server
			os.Setenv("F8_AUTH_URL", tt.URL)

			c := NewAuthServiceTokenClient(config)

			got, err := c.Get(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("ServiceAccountTokenClient.Get() error = %v, wantErr %v", err, tt.wantErr)
				return
			} else if err != nil && tt.wantErr {
				t.Logf("ServiceAccountTokenClient.Get() failed with = %v", err)
				return
			}
			if got != want {
				t.Errorf("ServiceAccountTokenClient.Get() = %v, want %v", got, want)
			}
		})
	}
}

func Test_validateError(t *testing.T) {

	config, err := configuration.GetData()
	if err != nil {
		t.Fatalf("could not retrieve configuration: %v", err)
	}

	authclient, err := CreateClient(config)
	if err != nil {
		t.Fatalf("%v", err)
	}

	tests := []struct {
		name    string
		res     *http.Response
		wantErr bool
	}{
		{
			name: "status ok",
			res: &http.Response{
				StatusCode: http.StatusOK,
			},
		},
		{
			name: "unmarshalling should fail",
			res: &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       ioutil.NopCloser(bytes.NewReader([]byte("foo"))),
			},
			wantErr: true,
		},
		{
			name: "error response should be parsed rightly",
			res: &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body: ioutil.NopCloser(bytes.NewReader([]byte(`
				{
					"errors": [
						{
								"code": "jwt_security_error",
								"detail": "JWT validation failed: token contains an invalid number of segments",
								"id": "BEO45Wxi",
								"status": "401",
								"title": "Unauthorized"
						}
					]
				}`))),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateError(authclient, tt.res); (err != nil) != tt.wantErr {
				t.Errorf("validateError() error = %v, wantErr %v", err, tt.wantErr)
			} else if err != nil && tt.wantErr {
				t.Logf("validateError() failed with error = %v", err)
			}
		})
	}
}
