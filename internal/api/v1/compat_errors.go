package v1

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// compatErrors renders errors in the client's negotiated shape: OpenAI
// {error:{...}} by default, Anthropic {type:error,error:{type,message}}
// when the anthropic-version header opts in. Retry-After passes through on
// both shapes.
//
// Shape contract (documentation only, wire unchanged):
//   - OpenAI shape: {"error":{"message","type","param","code"}} with HTTP
//     status from the router classification. Param names the offending
//     field; absent on Anthropic.
//   - Anthropic shape: {"type":"error","error":{"type","message"}} with
//     status from AnthropicStatus and type from AnthropicErrorType. The
//     version header is accepted and echoed, never pinned.
// Callers pick the envelope via writeCompatError; logs always use the
// shared keys endpoint, code, model, provider_id and error.

// compatErrors renders errors in the client's negotiated shape: OpenAI
// {error:{...}} by default, Anthropic {type:error,error:{type,message}}
// when the anthropic-version header opts in. Retry-After passes through on
// both shapes.

// writeCompatError writes one error in the negotiated envelope. Param names
// the offending field on OpenAI shape only; Anthropic has no param member.
func (h *Handler) writeCompatError(w http.ResponseWriter, r *http.Request, status int, code, message string, parameter *string) {
	echoAnthropicVersion(w, r)
	if !isAnthropicStyle(r) {
		h.writeError(w, status, code, message, parameter)
		return
	}
	h.writeJSON(w, apierrors.AnthropicStatus(code, status), map[string]any{
		"type":  "error",
		"error": map[string]any{"type": apierrors.AnthropicErrorType(code), "message": message},
	})
}

// handleCompatRouterError renders a router error in the negotiated
// envelope. The plugin already produced the terminal OpenAI-shaped error,
// so the layer only translates the envelope, never reclassifies. Wire only:
// callers record "Request failed" via logRouterFailure with endpoint, code,
// model, provider_id and error before calling.
func (h *Handler) handleCompatRouterError(w http.ResponseWriter, r *http.Request, err error) {
	routerError := h.classifyError(err)
	apierrors.WriteResponseHeaders(w, pluginResponseHeaders(err))
	h.writeCompatError(w, r, routerError.status, routerError.code, wireMessage(err), nil)
}

// writeCompatDecodeError maps body decode failures in the negotiated
// envelope: oversized bodies are 413, everything else keeps 400.
func (h *Handler) writeCompatDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	h.logDecodeFailure(r, err)
	h.writeCompatDecodeErrorWithoutLog(w, r, err)
}

// writeCompatDecodeErrorWithoutLog renders the decode envelope without
// logging: writeDecodeError already logged with the real request while
// forcing the OpenAI branch with a headerless request.
func (h *Handler) writeCompatDecodeErrorWithoutLog(w http.ResponseWriter, r *http.Request, err error) {
	if isBodyTooLarge(err) {
		h.writeCompatError(w, r, http.StatusRequestEntityTooLarge, apierrors.CodeInvalidRequest, "request body too large", nil)
		return
	}
	h.writeCompatError(w, r, http.StatusBadRequest, apierrors.CodeInvalidRequest, "malformed request body: "+err.Error(), nil)
}

// requirePathID reads a path parameter once for all compat handlers.
// Empty values render invalid_request_error and report false.
func (h *Handler) requirePathID(w http.ResponseWriter, r *http.Request, name, label string) (string, bool) {
	id := strings.TrimSpace(r.PathValue(name))
	if id == "" {
		h.logValidationFailure(r, "", apierrors.CodeInvalidRequest)
		h.writeCompatError(w, r, http.StatusBadRequest, apierrors.CodeInvalidRequest, label+" id is required", nil)
		return "", false
	}
	return id, true
}

// validateEnumFormat validates one response/encoding format value against
// its endpoint set. Empty passes (server default); unknown values deny.
func validateEnumFormat(field, value string, allowed []string) error {
	if value == "" || slices.Contains(allowed, value) {
		return nil
	}
	return fmt.Errorf("invalid %s %q", field, value)
}

// echoAnthropicVersion reflects the negotiated version header. The value is
// never pinned: any non-empty version is accepted.
func echoAnthropicVersion(w http.ResponseWriter, r *http.Request) {
	if value := r.Header.Get("anthropic-version"); value != "" {
		w.Header().Set("anthropic-version", value)
	}
}

// endpointFromRequest returns the request path for log keys.
func endpointFromRequest(request *http.Request) string {
	if request == nil || request.URL == nil {
		return ""
	}
	return request.URL.Path
}

// appendModelFields adds model and provider_id keys when the model is set.
func appendModelFields(fields []any, model models.ModelId) []any {
	if model == "" {
		return fields
	}
	fields = append(fields, "model", string(model))
	if providerID, _, _ := model.Parse(); providerID != "" {
		fields = append(fields, "provider_id", providerID)
	}
	return fields
}

// logValidationFailure records a handled 400 validation rejection.
func (h *Handler) logValidationFailure(request *http.Request, model models.ModelId, code string) {
	if h == nil || h.logger == nil {
		return
	}
	fields := []any{"endpoint", endpointFromRequest(request), "code", code}
	fields = appendModelFields(fields, model)
	h.logger.Warn("Request validation failed", fields...)
}

// logDenyFailure records a handled 403 token-rules denial.
func (h *Handler) logDenyFailure(request *http.Request, model models.ModelId, deny *authorizationDeny) {
	if h == nil || h.logger == nil || deny == nil {
		return
	}
	fields := []any{"endpoint", endpointFromRequest(request), "code", deny.code}
	fields = appendModelFields(fields, model)
	h.logger.Warn("Request denied by token rules", fields...)
}

// logDecodeFailure records a handled body decode rejection.
func (h *Handler) logDecodeFailure(request *http.Request, decodeErr error) {
	if h == nil || h.logger == nil || decodeErr == nil {
		return
	}
	h.logger.Warn("Request decode failed",
		"endpoint", endpointFromRequest(request),
		"code", apierrors.CodeInvalidRequest,
		"error", decodeErr)
}

// logRouterFailure records a terminal router error with model context.
func (h *Handler) logRouterFailure(request *http.Request, model models.ModelId, routerErr error) {
	if h == nil || h.logger == nil || routerErr == nil {
		return
	}
	classified := apierrors.ToAPIError(routerErr)
	fields := []any{"endpoint", endpointFromRequest(request), "code", classified.Code, "error", routerErr}
	fields = appendModelFields(fields, model)
	h.logger.Error("Request failed", fields...)
}

// logAuthenticationFailure records a handled 401 rejection.
func (h *Handler) logAuthenticationFailure(request *http.Request, code string) {
	if h == nil || h.logger == nil {
		return
	}
	h.logger.Warn("Authentication failed",
		"endpoint", endpointFromRequest(request),
		"code", code)
}

// logTokenValidationFailure records a terminal 500 token failure.
func (h *Handler) logTokenValidationFailure(request *http.Request, validationErr error) {
	if h == nil || h.logger == nil || validationErr == nil {
		return
	}
	h.logger.Error("Token validation failed",
		"endpoint", endpointFromRequest(request),
		"code", apierrors.CodeServerError,
		"error", validationErr)
}
