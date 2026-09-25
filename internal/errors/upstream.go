package errors

import (
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// MapUpstream builds a ProviderError from an upstream HTTP status plus the
// structured code/type of the upstream error envelope. Matching is exact:
// status first, then known code/type values. Message text never decides.
func MapUpstream(status int, code, errType, message string) *models.ProviderError {
	msg := message
	if msg == "" {
		msg = "upstream request failed"
	}
	perr := &models.ProviderError{StatusCode: status, Message: msg}
	switch {
	case status == 401 || status == 403:
		perr.Type = models.ErrorTypeAuth
	case status == 404:
		perr.Type = models.ErrorTypeNotFound
	case status == 408 || status == 504:
		perr.Type = models.ErrorTypeTimeout
	case status == 429 && isQuotaCode(code, errType):
		perr.Type = models.ErrorTypeQuotaExceeded
	case status == 429:
		perr.Type = models.ErrorTypeRateLimit
	case status >= 500:
		perr.Type = models.ErrorTypeUpstream
	case status == 400:
		perr.Type = models.ErrorTypeInvalidRequest
	case isTimeoutCode(code, errType):
		perr.Type = models.ErrorTypeTimeout
	case isQuotaCode(code, errType):
		perr.Type = models.ErrorTypeQuotaExceeded
	default:
		perr.Type = models.ErrorTypeInvalidRequest
	}
	return perr
}

// isQuotaCode matches exact upstream quota identifiers.
func isQuotaCode(code, errType string) bool {
	switch code {
	case "insufficient_quota", "billing_hard_limit_exceeded", "out_of_quota", "quota_exceeded":
		return true
	}
	return errType == "insufficient_quota"
}

// isTimeoutCode matches exact upstream timeout identifiers.
func isTimeoutCode(code, errType string) bool {
	switch code {
	case "timeout", "request_timeout", "deadline_exceeded":
		return true
	}
	return errType == "timeout"
}
