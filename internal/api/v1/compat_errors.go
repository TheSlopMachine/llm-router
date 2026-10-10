package v1

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

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

// handleCompatRouterError renders a router error in the negotiated
// envelope. The plugin already produced the terminal OpenAI-shaped error,
// so the layer only translates the envelope, never reclassifies.
func (h *Handler) handleCompatRouterError(w http.ResponseWriter, r *http.Request, err error) {
	re := h.classifyError(err)
	apierrors.WriteResponseHeaders(w, pluginResponseHeaders(err))
	h.writeCompatError(w, r, re.status, re.code, wireMessage(err), nil)
}

// writeCompatDecodeError maps body decode failures in the negotiated
// envelope: oversized bodies are 413, everything else keeps 400.
func (h *Handler) writeCompatDecodeError(w http.ResponseWriter, r *http.Request, err error) {
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
	if v := r.Header.Get("anthropic-version"); v != "" {
		w.Header().Set("anthropic-version", v)
	}
}
