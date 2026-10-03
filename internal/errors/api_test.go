package errors

import (
	"errors"
	"net/http"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestToAPIErrorTable(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"provider not found", ErrProviderNotFound, http.StatusBadRequest, "provider_not_found"},
		{"model disabled", ErrModelDisabled, http.StatusNotFound, "model_not_found"},
		{"endpoint unsupported", ErrEndpointNotSupported, http.StatusBadRequest, "endpoint_not_supported"},
		{"no credential", ErrNoCredential, http.StatusServiceUnavailable, "no_credential"},
		{"provider disabled", ErrProviderDisabled, http.StatusBadRequest, "provider_disabled"},
		{"model not allowed", ErrModelNotAllowed, http.StatusForbidden, "model_not_allowed"},
		{"unauthorized", ErrUnauthorized, http.StatusUnauthorized, "auth_error"},
		{"timeout sentinel", ErrTimeout, http.StatusBadGateway, "timeout"},
		{"rate limited sentinel", ErrRateLimited, http.StatusBadGateway, "rate_limit"},
		{"unknown falls back", errors.New("weird transport failure"), http.StatusBadGateway, "upstream_error"},
		{"timeout text is not sniffed", errors.New("request timeout exceeded"), http.StatusBadGateway, "upstream_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ToAPIError(tc.err)
			if got.Status != tc.status || got.Code != tc.code {
				t.Errorf("got %+v, want status=%d code=%q", got, tc.status, tc.code)
			}
		})
	}
}

func TestToAPIErrorProviderErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    *models.ProviderError
		status int
		code   string
	}{
		{"verbatim passthrough", &models.ProviderError{StatusCode: 429, Message: "slow", Code: "rate_limit"}, 429, "rate_limit"},
		{"invalid request", &models.ProviderError{StatusCode: 400, Message: "bad", Code: "invalid_request_error"}, 400, "invalid_request_error"},
		{"default code", &models.ProviderError{StatusCode: 502, Message: "boom"}, 502, "server_error"},
		{"out of range status", &models.ProviderError{StatusCode: 200, Message: "weird", Code: "server_error"}, 502, "server_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ToAPIError(tc.err)
			if got.Status != tc.status || got.Code != tc.code {
				t.Errorf("got %+v, want status=%d code=%q", got, tc.status, tc.code)
			}
		})
	}
}
