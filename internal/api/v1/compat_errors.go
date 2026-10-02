package v1

import (
	"net/http"
	"strconv"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
)

// compatErrors renders errors in the client's negotiated shape: OpenAI
// {error:{...}} by default, Anthropic {type:error,error:{type,message}}
// when the anthropic-version header opts in. Retry-After passes through on
// both shapes.

// writeCompatError writes one error in the negotiated envelope. Param names
// the offending field on OpenAI shape only; Anthropic has no param member.
func (h *Handler) writeCompatError(w http.ResponseWriter, r *http.Request, status int, code, msg string, param *string) {
	echoAnthropicVersion(w, r)
	if !isAnthropicStyle(r) {
		h.writeError(w, status, code, msg, param)
		return
	}
	h.writeJSON(w, apierrors.AnthropicStatus(code, status), map[string]any{
		"type":  "error",
		"error": map[string]any{"type": apierrors.AnthropicErrorType(code), "message": msg},
	})
}

// handleCompatRouterError maps a router error through the shared classifier
// and writes it in the negotiated envelope.
func (h *Handler) handleCompatRouterError(w http.ResponseWriter, r *http.Request, err error) {
	re := h.classifyError(err)
	if secs, ok := apierrors.RetryAfterDelay(err); ok {
		w.Header().Set("Retry-After", strconv.FormatInt(secs, 10))
	}
	h.writeCompatError(w, r, re.status, re.code, err.Error(), nil)
}

// writeCompatDecodeError maps body decode failures in the negotiated
// envelope: oversized bodies are 413, everything else keeps 400.
func (h *Handler) writeCompatDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	if isBodyTooLarge(err) {
		h.writeCompatError(w, r, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body too large", nil)
		return
	}
	h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "malformed request body: "+err.Error(), nil)
}

// echoAnthropicVersion reflects the negotiated version header. The value is
// never pinned: any non-empty version is accepted.
func echoAnthropicVersion(w http.ResponseWriter, r *http.Request) {
	if v := r.Header.Get("anthropic-version"); v != "" {
		w.Header().Set("anthropic-version", v)
	}
}
