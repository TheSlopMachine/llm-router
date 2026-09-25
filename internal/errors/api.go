package errors

import (
	"errors"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// APIError is the transport mapping of a domain error: HTTP status plus the
// wire code. Both API surfaces (OpenAI /v1 and dashboard) render from it.
type APIError struct {
	Status int
	Code   string
}

// ToAPIError maps any domain error to its wire representation. Single source
// of truth for status/code selection; no message-substring matching.
func ToAPIError(err error) APIError {
	var provErr *models.ProviderError
	if errors.As(err, &provErr) {
		switch provErr.Type {
		case models.ErrorTypeRateLimit:
			return APIError{http.StatusBadGateway, "rate_limit"}
		case models.ErrorTypeQuotaExceeded:
			return APIError{http.StatusBadGateway, "quota_exceeded"}
		case models.ErrorTypeAuth:
			return APIError{http.StatusUnauthorized, "auth_error"}
		case models.ErrorTypeTimeout:
			return APIError{http.StatusBadGateway, "timeout"}
		case models.ErrorTypeNotFound:
			return APIError{http.StatusNotFound, "not_found"}
		case models.ErrorTypeInvalidRequest:
			return APIError{http.StatusBadRequest, "invalid_request_error"}
		case models.ErrorTypeGeo:
			return APIError{http.StatusBadRequest, "geo_blocked"}
		case models.ErrorTypePaymentRequired:
			return APIError{http.StatusPaymentRequired, "payment_required"}
		case models.ErrorTypeUpstream:
			return APIError{http.StatusBadGateway, "upstream_error"}
		default:
			return APIError{http.StatusBadGateway, "upstream_error"}
		}
	}
	switch {
	case errors.Is(err, ErrProviderNotFound):
		return APIError{http.StatusBadRequest, "provider_not_found"}
	case errors.Is(err, ErrModelDisabled):
		return APIError{http.StatusNotFound, "model_not_found"}
	case errors.Is(err, ErrEndpointNotSupported):
		return APIError{http.StatusBadRequest, "endpoint_not_supported"}
	case errors.Is(err, ErrNoCredential):
		return APIError{http.StatusServiceUnavailable, "no_credential"}
	case errors.Is(err, ErrProviderDisabled):
		return APIError{http.StatusBadRequest, "provider_disabled"}
	case errors.Is(err, ErrModelNotAllowed):
		return APIError{http.StatusForbidden, "model_not_allowed"}
	case errors.Is(err, ErrProviderNotAllowed):
		return APIError{http.StatusForbidden, "provider_not_allowed"}
	case errors.Is(err, ErrCredentialNotAllowed):
		return APIError{http.StatusForbidden, "credential_not_allowed"}
	case errors.Is(err, ErrUnauthorized):
		return APIError{http.StatusUnauthorized, "auth_error"}
	case errors.Is(err, ErrTimeout):
		return APIError{http.StatusBadGateway, "timeout"}
	case errors.Is(err, ErrRateLimited):
		return APIError{http.StatusBadGateway, "rate_limit"}
	default:
		return APIError{http.StatusBadGateway, "upstream_error"}
	}
}

// ErrorTypeForCode maps a wire code to its OpenAI error type.
func ErrorTypeForCode(code string) string {
	switch code {
	case "invalid_request_error", "not_found", "model_not_allowed", "provider_not_allowed", "credential_not_allowed", "provider_not_found":
		return "invalid_request_error"
	case "missing_token", "invalid_token", "auth_error", "payment_required":
		return "invalid_request_error"
	case "rate_limit", "quota_exceeded":
		return "rate_limit_error"
	case "server_error", "upstream_error", "timeout", "internal_error":
		return "server_error"
	default:
		return "invalid_request_error"
	}
}
