package errors

import (
	"errors"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// Wire error codes. Single source of truth for status/code strings
// rendered on both API surfaces.
const (
	CodeInvalidRequest      = "invalid_request_error"
	CodeNotFound            = "not_found"
	CodeProviderNotFound    = "provider_not_found"
	CodeModelNotFound       = "model_not_found"
	CodeEndpointUnsupported = "endpoint_not_supported"
	CodeNoCredential        = "no_credential"
	CodeProviderDisabled    = "provider_disabled"
	CodeModelNotAllowed     = "model_not_allowed"
	CodeProviderNotAllowed  = "provider_not_allowed"
	CodeCredentialNotAllow  = "credential_not_allowed"
	CodeAuthError           = "auth_error"
	CodeMissingToken        = "missing_token"
	CodeInvalidToken        = "invalid_token"
	CodePaymentRequired     = "payment_required"
	CodeRateLimit           = "rate_limit"
	CodeQuotaExceeded       = "quota_exceeded"
	CodeOverloaded          = "overloaded"
	CodeTimeout             = "timeout"
	CodeTransportError      = "transport_error"
	CodeUpstreamError       = "upstream_error"
	CodeModelUnavailable    = "model_unavailable"
	CodeStructuralFault     = "structural_fault"
	CodeServerError         = "server_error"
	CodeInternalError       = "internal_error"
)

// APIError is the transport mapping of a domain error: HTTP status plus the
// wire code. Both API surfaces (OpenAI /v1 and dashboard) render from it.
type APIError struct {
	Status int
	Code   string
}

// wireCodeMeta consolidates the three code tables below: Anthropic error
// type, OpenAI error type, and Anthropic status override (0 keeps status).
type wireCodeMeta struct {
	anthropicType string
	openAIType    string
	anthropicHTTP int
}

var wireMeta = map[string]wireCodeMeta{
	CodeInvalidRequest:     {anthropicType: CodeInvalidRequest, openAIType: CodeInvalidRequest},
	CodeNotFound:           {anthropicType: CodeInvalidRequest, openAIType: CodeInvalidRequest},
	CodeModelNotAllowed:    {anthropicType: CodeInvalidRequest, openAIType: CodeInvalidRequest},
	CodeProviderNotAllowed: {anthropicType: CodeInvalidRequest, openAIType: CodeInvalidRequest},
	CodeCredentialNotAllow: {anthropicType: CodeInvalidRequest, openAIType: CodeInvalidRequest},
	CodeProviderNotFound:   {anthropicType: CodeInvalidRequest, openAIType: CodeInvalidRequest},
	CodeAuthError:          {anthropicType: "authentication_error", openAIType: CodeInvalidRequest},
	CodeMissingToken:       {anthropicType: "authentication_error", openAIType: CodeInvalidRequest},
	CodeInvalidToken:       {anthropicType: "authentication_error", openAIType: CodeInvalidRequest},
	CodePaymentRequired:    {anthropicType: "permission_error", openAIType: CodeInvalidRequest},
	CodeRateLimit:          {anthropicType: "rate_limit_error", openAIType: "rate_limit_error", anthropicHTTP: 429},
	CodeQuotaExceeded:      {anthropicType: "rate_limit_error", openAIType: "rate_limit_error", anthropicHTTP: 429},
	CodeOverloaded:         {anthropicType: "overloaded_error", openAIType: CodeServerError, anthropicHTTP: 529},
	CodeTimeout:            {anthropicType: "api_error", openAIType: "api_error"},
	CodeTransportError:     {anthropicType: "api_error", openAIType: CodeServerError},
	CodeUpstreamError:      {anthropicType: "api_error", openAIType: CodeServerError},
	CodeModelUnavailable:   {anthropicType: "api_error", openAIType: CodeServerError},
	CodeStructuralFault:    {anthropicType: "api_error", openAIType: CodeServerError},
	CodeServerError:        {anthropicType: "api_error", openAIType: CodeServerError},
	CodeInternalError:      {anthropicType: "api_error", openAIType: CodeServerError},
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
			code = CodeServerError
		}
		return APIError{Status: status, Code: code}
	}
	switch {
	case errors.Is(err, ErrProviderNotFound):
		return APIError{http.StatusBadRequest, CodeProviderNotFound}
	case errors.Is(err, ErrModelDisabled):
		return APIError{http.StatusNotFound, CodeModelNotFound}
	case errors.Is(err, ErrEndpointNotSupported):
		return APIError{http.StatusBadRequest, CodeEndpointUnsupported}
	case errors.Is(err, ErrNoCredential):
		return APIError{http.StatusServiceUnavailable, CodeNoCredential}
	case errors.Is(err, ErrProviderDisabled):
		return APIError{http.StatusBadRequest, CodeProviderDisabled}
	case errors.Is(err, ErrModelNotAllowed):
		return APIError{http.StatusForbidden, CodeModelNotAllowed}
	case errors.Is(err, ErrProviderNotAllowed):
		return APIError{http.StatusForbidden, CodeProviderNotAllowed}
	case errors.Is(err, ErrCredentialNotAllowed):
		return APIError{http.StatusForbidden, CodeCredentialNotAllow}
	case errors.Is(err, ErrUnauthorized):
		return APIError{http.StatusUnauthorized, CodeAuthError}
	case errors.Is(err, ErrTimeout):
		return APIError{http.StatusBadGateway, CodeTimeout}
	case errors.Is(err, ErrRateLimited):
		return APIError{http.StatusBadGateway, CodeRateLimit}
	default:
		return APIError{http.StatusBadGateway, CodeUpstreamError}
	}
}

// AnthropicErrorType maps a wire code to its Anthropic error type. Single
// source of truth for the negotiated {type:error,error:{type,message}}
// envelope served on Anthropic-style requests.
func AnthropicErrorType(code string) string {
	if m, ok := wireMeta[code]; ok {
		return m.anthropicType
	}
	return "api_error"
}

// AnthropicStatus maps a wire status to the Anthropic HTTP status: rate
// limits surface as 429 and congestion as 529; everything else keeps the
// shared status.
func AnthropicStatus(code string, status int) int {
	if m, ok := wireMeta[code]; ok && m.anthropicHTTP != 0 {
		return m.anthropicHTTP
	}
	return status
}

// ErrorTypeForCode maps a wire code to its OpenAI error type.
func ErrorTypeForCode(code string) string {
	if m, ok := wireMeta[code]; ok {
		return m.openAIType
	}
	return CodeInvalidRequest
}
