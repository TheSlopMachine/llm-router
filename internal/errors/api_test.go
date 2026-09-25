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

func TestToAPIErrorProviderTypes(t *testing.T) {
	cases := []struct {
		typ    models.ErrorType
		status int
		code   string
	}{
		{models.ErrorTypeRateLimit, http.StatusBadGateway, "rate_limit"},
		{models.ErrorTypeQuotaExceeded, http.StatusBadGateway, "quota_exceeded"},
		{models.ErrorTypeAuth, http.StatusUnauthorized, "auth_error"},
		{models.ErrorTypeTimeout, http.StatusBadGateway, "timeout"},
		{models.ErrorTypeNotFound, http.StatusNotFound, "not_found"},
		{models.ErrorTypeInvalidRequest, http.StatusBadRequest, "invalid_request_error"},
		{models.ErrorTypeGeo, http.StatusBadRequest, "geo_blocked"},
		{models.ErrorTypePaymentRequired, http.StatusPaymentRequired, "payment_required"},
		{models.ErrorTypeUpstream, http.StatusBadGateway, "upstream_error"},
	}
	for _, tc := range cases {
		got := ToAPIError(&models.ProviderError{StatusCode: 500, Type: tc.typ})
		if got.Status != tc.status || got.Code != tc.code {
			t.Errorf("type %d: got %+v, want status=%d code=%q", tc.typ, got, tc.status, tc.code)
		}
	}
}

func TestMapUpstreamExactCodes(t *testing.T) {
	quota := MapUpstream(429, "insufficient_quota", "", "quota msg")
	if quota.Type != models.ErrorTypeQuotaExceeded {
		t.Errorf("quota code: got %v", quota.Type)
	}
	plain := MapUpstream(429, "rate_limit_exceeded", "rate_limit", "slow down")
	if plain.Type != models.ErrorTypeRateLimit {
		t.Errorf("rate limit code: got %v", plain.Type)
	}
	// Message text alone never decides: quota word without exact code stays rate limit.
	textOnly := MapUpstream(429, "", "", "quota exceeded for key")
	if textOnly.Type != models.ErrorTypeRateLimit {
		t.Errorf("text-only quota: got %v", textOnly.Type)
	}
	timeout := MapUpstream(504, "", "", "gateway timeout")
	if timeout.Type != models.ErrorTypeTimeout {
		t.Errorf("504: got %v", timeout.Type)
	}
	notFound := MapUpstream(404, "", "", "no such model")
	if notFound.Type != models.ErrorTypeNotFound {
		t.Errorf("404: got %v", notFound.Type)
	}
	payment := MapUpstream(402, "", "", "subscription required")
	if payment.Type != models.ErrorTypePaymentRequired {
		t.Errorf("402: got %v", payment.Type)
	}
}
