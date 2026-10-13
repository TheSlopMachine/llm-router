package router

import (
	"context"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/healthcheck"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

// TestResult reports the outcome of a credential or model probe.
// QuotaExceeded marks temporary exhaustion: the model is alive,
// the quota is not. Callers must not disable models over it.
// Code is the machine-readable wire code for the failure (empty on
// success); Summary is a short human string for toasts, the full error
// goes to logs.
type TestResult struct {
	OK            bool   `json:"ok"`
	Latency       int64  `json:"latency_ms"`
	Error         string `json:"error,omitempty"`
	Response      string `json:"response,omitempty"`
	QuotaExceeded bool   `json:"quota_exceeded,omitempty"`
	Code          string `json:"code,omitempty"`
	Summary       string `json:"summary,omitempty"`
	Proxy         string `json:"proxy,omitempty"`
}

// TestCredential verifies one credential through the type's check_health
// handler. Types without the handler report unsupported: no live request
// runs, so probing never spends quota or triggers traffic side effects. An
// explicit unhealthy verdict disables the credential at once
// (DisabledBy="healthcheck"); unknown and handler failures change nothing.
// overrideModel is accepted for endpoint compatibility and ignored.
func (s *Service) TestCredential(ctx context.Context, providerID, credentialID string, overrideModel string) TestResult {
	resolved, err := provider.Resolve(s.providerService, ctx, providerID)
	if err != nil {
		s.debug("Probing credential fails for unknown provider",
			"provider_id", providerID, "cred_id", credentialID,
			"reason", "provider_not_found", "error", err)
		return TestResult{Error: err.Error(), Code: apierrors.ToAPIError(err).Code}
	}
	credential, err := s.credentialService.Get(ctx, credentialID)
	if err != nil {
		s.debug("Probing credential fails for missing credential",
			"provider_id", providerID, "cred_id", credentialID,
			"reason", "credential_not_found", "error", err)
		return TestResult{Error: "credential not found", Code: "not_found"}
	}
	if credential.ProviderID != providerID {
		s.debug("Probing credential fails for mismatched provider",
			"provider_id", providerID, "cred_id", credentialID,
			"reason", "credential_provider_mismatch", "detail", credential.ProviderID)
		return TestResult{Error: "credential does not belong to this provider", Code: "invalid_request_error"}
	}
	luaService := s.providerService.LuaService()
	if luaService == nil || !luaService.HasHandler(resolved.Instance.TypeKey, string(luaplugin.HandlerCheckHealth)) {
		s.debug("Probing credential skips unsupported health check",
			"provider_id", providerID, "type", resolved.Instance.TypeKey, "cred_id", credentialID,
			"reason", "healthcheck_unsupported")
		return TestResult{Error: "health check not supported for this provider", Code: "unsupported", Summary: "health check not supported"}
	}
	start := time.Now()
	checkCtx, cancel := context.WithTimeout(ctx, healthcheck.CheckTimeout)
	defer cancel()
	status, message, healthError := luaService.CheckHealth(checkCtx, resolved.Instance.TypeKey, credential)
	latency := time.Since(start).Milliseconds()
	if healthError != nil {
		s.debug("Probing credential health check fails",
			"provider_id", providerID, "type", resolved.Instance.TypeKey, "cred_id", credentialID,
			"latency_ms", latency, "error", healthError)
		return TestResult{Error: healthError.Error(), Code: "upstream_error", Summary: "health check failed", Latency: latency}
	}
	switch status {
	case luaplugin.HealthHealthy:
		s.info("Probing credential reports healthy",
			"provider_id", providerID, "type", resolved.Instance.TypeKey, "cred_id", credentialID,
			"latency_ms", latency)
		return TestResult{OK: true, Latency: latency, Response: "healthy"}
	case luaplugin.HealthUnhealthy:
		reason := message
		if reason == "" {
			reason = "health check reported unhealthy"
		}
		// The provider automation switch masters every non-manual disable:
		// off reports the verdict without touching the credential.
		if !disableFailedCredentials(resolved) {
			s.warn("Probing credential reports unhealthy",
				"provider_id", providerID, "type", resolved.Instance.TypeKey, "cred_id", credentialID,
				"reason", reason, "latency_ms", latency)
			return TestResult{Error: reason, Code: "unhealthy", Summary: "credential unhealthy", Latency: latency}
		}
		if disableError := s.credentialService.DisableUnhealthy(ctx, credentialID, reason); disableError != nil {
			s.logError("Disabling unhealthy credential fails",
				"provider_id", providerID, "type", resolved.Instance.TypeKey, "cred_id", credentialID,
				"operation", "test_credential", "reason", reason, "error", disableError)
			return TestResult{Error: disableError.Error(), Code: "upstream_error", Summary: "disable failed", Latency: latency}
		}
		s.warn("Disabling unhealthy credential after probe",
			"provider_id", providerID, "type", resolved.Instance.TypeKey, "cred_id", credentialID,
			"reason", reason, "latency_ms", latency)
		return TestResult{Error: reason, Code: "unhealthy", Summary: "credential unhealthy, disabled", Latency: latency}
	default:
		if message == "" {
			message = "health check inconclusive"
		}
		s.debug("Probing credential reports inconclusive result",
			"provider_id", providerID, "type", resolved.Instance.TypeKey, "cred_id", credentialID,
			"reason", message, "latency_ms", latency)
		return TestResult{Error: message, Code: "unknown", Summary: "health check inconclusive", Latency: latency}
	}
}

// logProbeResult records the probe mapping outcome. Quota exhaustion stays
// visible at info so callers never mistake a live model for a dead one.
func (s *Service) logProbeResult(model models.ModelId, result TestResult) {
	arguments := []any{"model", model.String(), "latency_ms", result.Latency}
	if result.Proxy != "" {
		arguments = append(arguments, "proxy_url", result.Proxy)
	}
	switch {
	case result.OK:
		s.info("Probing model succeeds", arguments...)
	case result.QuotaExceeded:
		s.info("Probing model reports quota exhaustion",
			append(arguments, "code", result.Code, "reason", "quota_exhausted", "error", result.Error)...)
	default:
		s.debug("Probing model fails",
			append(arguments, "code", result.Code, "reason", result.Summary, "error", result.Error)...)
	}
}

// TestModel verifies one model through the type's check_model handler.
// The plugin owns liveness payloads for every endpoint; the router owns
// resolution, timeout, latency and verdict mapping only. Types without the
// handler and built-in Go backends report unsupported with no live request.
// Manually disabled models stay testable. Probes never mutate state and
// never disable models; the dashboard disables explicitly on
// model_unavailable verdicts.
func (s *Service) TestModel(ctx context.Context, modelID models.ModelId, endpoint string) TestResult {
	if endpoint == "" {
		endpoint = "chat"
	}
	start := time.Now()
	providerID, _, err := modelID.Parse()
	if err != nil {
		s.debug("Probing model fails for invalid model identifier",
			"model", modelID.String(), "endpoint", endpoint,
			"reason", "invalid_model_id", "error", err)
		result := TestResult{Error: err.Error(), Code: apierrors.ToAPIError(err).Code, Latency: time.Since(start).Milliseconds(), Summary: "probe failed"}
		s.logProbeResult(modelID, result)
		return result
	}
	resolved, err := provider.Resolve(s.providerService, ctx, providerID)
	if err != nil {
		s.debug("Probing model fails for unknown provider",
			"provider_id", providerID, "model", modelID.String(), "endpoint", endpoint,
			"reason", "provider_not_found", "error", err)
		result := TestResult{Error: err.Error(), Code: apierrors.ToAPIError(err).Code, Latency: time.Since(start).Milliseconds(), Summary: "probe failed"}
		s.logProbeResult(modelID, result)
		return result
	}
	if !resolved.IsLua() {
		s.debug("Probing model skips unsupported model check",
			"provider_id", providerID, "type", resolved.Instance.TypeKey, "model", modelID.String(), "endpoint", endpoint,
			"reason", "modelcheck_unsupported")
		result := TestResult{Error: "health check not supported for this provider", Code: "unsupported", Summary: "health check not supported", Latency: time.Since(start).Milliseconds()}
		s.logProbeResult(modelID, result)
		return result
	}
	luaService := s.providerService.LuaService()
	if luaService == nil || !luaService.HasHandler(resolved.Instance.TypeKey, string(luaplugin.HandlerCheckModel)) {
		s.debug("Probing model skips unsupported model check",
			"provider_id", providerID, "type", resolved.Instance.TypeKey, "model", modelID.String(), "endpoint", endpoint,
			"reason", "modelcheck_unsupported")
		result := TestResult{Error: "health check not supported for this provider", Code: "unsupported", Summary: "health check not supported", Latency: time.Since(start).Milliseconds()}
		s.logProbeResult(modelID, result)
		return result
	}
	metadata := luaplugin.HandlerMeta{
		ProviderID:     resolved.Instance.ID,
		TypeKey:        resolved.Instance.TypeKey,
		Model:          modelID,
		ProviderConfig: resolved.Instance.Config,
	}
	checkCtx, cancel := context.WithTimeout(ctx, healthcheck.CheckTimeout)
	defer cancel()
	health, checkError := luaService.CheckModel(checkCtx, metadata, endpoint)
	latency := time.Since(start).Milliseconds()
	if checkError != nil {
		s.debug("Probing model health check fails",
			"provider_id", providerID, "type", resolved.Instance.TypeKey, "model", modelID.String(), "endpoint", endpoint,
			"latency_ms", latency, "error", checkError)
		result := TestResult{Error: checkError.Error(), Code: "upstream_error", Summary: "health check failed", Latency: latency}
		s.logProbeResult(modelID, result)
		return result
	}
	var result TestResult
	switch health.Status {
	case luaplugin.ModelHealthy:
		response := health.Response
		if response == "" {
			response = "healthy"
		}
		result = TestResult{OK: true, Latency: latency, Response: response}
	case luaplugin.ModelQuotaExceeded:
		reason := health.Message
		if reason == "" {
			reason = "quota exceeded, temporary"
		}
		result = TestResult{Error: reason, Code: "quota_exceeded", Summary: "quota exceeded, temporary", Latency: latency, QuotaExceeded: true}
	case luaplugin.ModelUnhealthy:
		reason := health.Message
		if reason == "" {
			reason = "health check reported unhealthy"
		}
		result = TestResult{Error: reason, Code: "model_unavailable", Summary: "model unavailable", Latency: latency}
	default:
		reason := health.Message
		if reason == "" {
			reason = "health check inconclusive"
		}
		result = TestResult{Error: reason, Code: "unknown", Summary: "health check inconclusive", Latency: latency}
	}
	s.logProbeResult(modelID, result)
	return result
}

// disableFailedCredentials reports whether a provider opts into automatic
// credential disables. Absent means off, matching the dashboard default:
// probes and health checks report verdicts without mutating.
func disableFailedCredentials(resolved *provider.Resolved) bool {
	if resolved == nil || resolved.Instance == nil {
		return false
	}
	return models.CredentialAutomationOn(resolved.Instance.Config)
}
