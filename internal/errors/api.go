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
// ProviderError carries its own status and code: the plugin already
// produced the terminal OpenAI-shaped error, so the router renders it
// verbatim after sanitizing the status into range.
func ToAPIError(err error) APIError {
	var provErr *models.ProviderError
	if errors.As(err, &provErr) {
		status := provErr.StatusCode
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		code := provErr.Code
		if code == "" {
			code = "server_error"
		}
		return APIError{Status: status, Code: code}
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

// AnthropicErrorType maps a wire code to its Anthropic error type. Single
// source of truth for the negotiated {type:error,error:{type,message}}
// envelope served on Anthropic-style requests.
func AnthropicErrorType(code string) string {
	switch code {
	case "invalid_request_error", "not_found", "model_not_allowed", "provider_not_allowed", "credential_not_allowed", "provider_not_found":
		return "invalid_request_error"
	case "auth_error", "missing_token", "invalid_token":
		return "authentication_error"
	case "payment_required":
		return "permission_error"
	case "rate_limit", "quota_exceeded":
		return "rate_limit_error"
	case "overloaded":
		return "overloaded_error"
	case "timeout", "transport_error", "upstream_error", "model_unavailable", "structural_fault", "server_error", "internal_error":
		return "api_error"
	default:
		return "api_error"
	}
}

// AnthropicStatus maps a wire status to the Anthropic HTTP status: rate
// limits surface as 429 and congestion as 529; everything else keeps the
// shared status.
func AnthropicStatus(code string, status int) int {
	switch code {
	case "rate_limit", "quota_exceeded":
		return 429
	case "overloaded":
		return 529
	default:
		return status
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
	case "transport_error", "overloaded":
		return "server_error"
	case "server_error", "upstream_error", "model_unavailable", "structural_fault", "internal_error":
		return "server_error"
	default:
		return "invalid_request_error"
	}
}
