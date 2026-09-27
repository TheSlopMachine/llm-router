package errors

import (
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// MapUpstream builds a ProviderError from an upstream HTTP status plus the
// structured code/type of the upstream error envelope. Code/type fallbacks
// run before the bare-status default so non-standard upstreams signalling
// quota/content/model state through 400 are classified correctly instead of
// collapsing into invalid_request. Message text never decides, except the
// quota wording handled one layer up on bare 429s.
func MapUpstream(status int, code, errType, message string) *models.ProviderError {
	msg := message
	if msg == "" {
		msg = "upstream request failed"
	}
	perr := &models.ProviderError{StatusCode: status, Message: msg}
	switch {
	case status == 402:
		perr.Type = models.ErrorTypePaymentRequired
	case status == 401:
		perr.Type = models.ErrorTypeAuth
	case status == 403 && isContentPolicyCode(code, errType):
		perr.Type = models.ErrorTypeContentPolicy
	case status == 403 && isQuotaCode(code, errType):
		perr.Type = models.ErrorTypeQuotaExceeded
	case status == 403:
		perr.Type = models.ErrorTypeAuth
	case status == 404:
		perr.Type = models.ErrorTypeNotFound
	case status == 429 && isQuotaCode(code, errType):
		perr.Type = models.ErrorTypeQuotaExceeded
	case status == 429:
		perr.Type = models.ErrorTypeRateLimit
	case status >= 500 && isModelUnavailableCode(code, errType):
		perr.Type = models.ErrorTypeModelUnavailable
	case status >= 500:
		perr.Type = models.ErrorTypeUpstream
	case isQuotaCode(code, errType):
		perr.Type = models.ErrorTypeQuotaExceeded
	case isContentPolicyCode(code, errType):
		perr.Type = models.ErrorTypeContentPolicy
	case isModelUnavailableCode(code, errType):
		perr.Type = models.ErrorTypeModelUnavailable
	case status == 400:
		perr.Type = models.ErrorTypeInvalidRequest
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

// isContentPolicyCode matches exact upstream content/moderation identifiers.
func isContentPolicyCode(code, errType string) bool {
	switch code {
	case "content_filter", "content_filtered", "moderation_blocked", "policy_violation", "safety_violation":
		return true
	}
	switch errType {
	case "content_filter", "moderation", "policy_violation", "safety":
		return true
	}
	return false
}

// isModelUnavailableCode matches exact upstream model-loading identifiers:
// the model exists but is not serving (cold start, decommissioned, busy).
func isModelUnavailableCode(code, errType string) bool {
	switch code {
	case "model_loading", "model_not_ready", "model_busy", "engine_overloaded", "cold_start":
		return true
	}
	switch errType {
	case "model_loading", "model_unavailable":
		return true
	}
	return false
}
