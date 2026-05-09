// Package v1 implements the OpenAI-compatible /v1/... API endpoints.
//
// Incoming requests are:
//  1. Authenticated via the Internal Token Service
//  2. Validated against the token's rules (allowed models)
//  3. Routed to the appropriate provider by the Router Service
//  4. Translated back to OpenAI-compatible responses
package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/metrics"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
)

// Handler holds the dependencies for the v1 API.
type Handler struct {
	tokens  *token.Service
	router  *router.Service
	metrics *metrics.Service
	logger  *slog.Logger
}

// New constructs a v1 Handler.
func New(tokens *token.Service, routerSvc *router.Service, metricsSvc *metrics.Service, logger *slog.Logger) *Handler {
	return &Handler{
		tokens:  tokens,
		router:  routerSvc,
		metrics: metricsSvc,
		logger:  logger,
	}
}

// Register mounts all /v1 routes onto mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/chat/completions", h.auth(h.chatCompletions))
	mux.HandleFunc("GET /v1/models", h.auth(h.listModels))
}

// ─────────────────────────────────────────────
// Endpoints
// ─────────────────────────────────────────────

// chatCompletions handles POST /v1/chat/completions
// @Summary      Create chat completion
// @Description  Creates a completion for the chat message. Supports both streaming and non-streaming responses.
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        request body models.ChatCompletionRequest true "Chat completion request"
// @Success      200 {object} models.ChatCompletionResponse "Successful response"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      403 {object} models.OpenAIError "Forbidden - model not allowed by token rules"
// @Failure      502 {object} models.OpenAIError "Bad Gateway - upstream provider error"
// @Failure      503 {object} models.OpenAIError "Service Unavailable - no credential available"
// @Security     BearerAuth
// @Router       /v1/chat/completions [post]
func (h *Handler) chatCompletions(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	var req models.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("malformed request body: %s", err))
		return
	}

	if !t.Rules.Allows(req.Model) {
		h.writeError(w, http.StatusForbidden, "model_not_allowed",
			fmt.Sprintf("model %q is not allowed by your token's rules", req.Model))
		return
	}

	if req.Stream {
		h.handleStreamWithMetrics(w, r, &req, t, start)
		return
	}

	resp, err := h.router.Complete(r.Context(), &req)
	duration := time.Since(start)

	// Extract provider info
	providerType, _, _ := req.Model.Parse()
	providerID, _ := h.router.GetProviderIDForModel(r.Context(), req.Model)

	// Build metric event
	event := models.MetricEvent{
		Timestamp:    start,
		ProviderID:   providerID,
		ProviderType: providerType,
		Model:        req.Model,
		TokenID:      t.ID,
		Duration:     duration,
		StatusCode:   http.StatusOK,
	}

	if err == nil && resp != nil && resp.Usage.TotalTokens > 0 {
		event.TokensInput = int64(resp.Usage.PromptTokens)
		event.TokensOutput = int64(resp.Usage.CompletionTokens)
	}

	if err != nil {
		re := h.classifyError(err)
		event.StatusCode = re.status
		event.ErrorType = re.code
	}

	// Record metrics (non-blocking)
	h.metrics.RecordRequest(event)

	if err != nil {
		h.handleRouterError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, resp)
}

// handleStreamWithMetrics wraps streaming with metrics collection.
func (h *Handler) handleStreamWithMetrics(w http.ResponseWriter, r *http.Request, req *models.ChatCompletionRequest, t *models.RouterToken, start time.Time) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "streaming_unsupported", "streaming is not supported by this server")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	err := h.router.CompleteStream(r.Context(), req, w)
	duration := time.Since(start)

	// Extract provider info
	providerType, _, _ := req.Model.Parse()
	providerID, _ := h.router.GetProviderIDForModel(r.Context(), req.Model)

	// Build metric event
	event := models.MetricEvent{
		Timestamp:    start,
		ProviderID:   providerID,
		ProviderType: providerType,
		Model:        req.Model,
		TokenID:      t.ID,
		Duration:     duration,
		StatusCode:   http.StatusOK,
	}

	if err != nil {
		event.StatusCode = http.StatusInternalServerError
		re := h.classifyError(err)
		event.ErrorType = re.code
		h.logger.Error("stream error", "err", err)
		
		// Send error in OpenAI-compatible format as SSE event
		errorObj := models.OpenAIError{
			Error: models.OpenAIErrorBody{
				Message: err.Error(),
				Type:    "error",
				Code:    re.code,
			},
		}
		errorJSON, _ := json.Marshal(errorObj)
		fmt.Fprintf(w, "data: %s\n\n", errorJSON)
		flusher.Flush()
	}

	// Record metrics
	h.metrics.RecordRequest(event)
}

// listModels handles GET /v1/models
// @Summary      List available models
// @Description  Lists all models available to the authenticated token based on its allowed models rules.
// @Tags         OpenAI API
// @Produce      json
// @Success      200 {object} object{object=string,data=[]object{id=string,object=string}} "List of models"
// @Failure      401 {object} models.OpenAIError "Unauthorized"
// @Security     BearerAuth
// @Router       /v1/models [get]
func (h *Handler) listModels(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	type modelEntry struct {
		ID     string `json:"id"`
		Object string `json:"object"`
	}
	type modelList struct {
		Object string       `json:"object"`
		Data   []modelEntry `json:"data"`
	}

	var entries []modelEntry
	for _, m := range t.Rules.AllowedModels {
		entries = append(entries, modelEntry{ID: string(m), Object: "model"})
	}

	h.writeJSON(w, http.StatusOK, modelList{Object: "list", Data: entries})
}

// ─────────────────────────────────────────────
// Middleware
// ─────────────────────────────────────────────

type authedHandler func(w http.ResponseWriter, r *http.Request, t *models.RouterToken)

// auth extracts and validates the Bearer token from Authorization header.
func (h *Handler) auth(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := extractBearer(r)
		if raw == "" {
			h.writeError(w, http.StatusUnauthorized, "missing_token", "Authorization: Bearer <token> header is required")
			return
		}

		t, err := h.tokens.Validate(raw)
		if err != nil {
			if errors.Is(err, apierrors.ErrUnauthorized) {
				h.writeError(w, http.StatusUnauthorized, "invalid_token", "invalid or revoked token")
				return
			}
			h.writeError(w, http.StatusInternalServerError, "internal_error", "token validation failed")
			return
		}

		next(w, r, t)
	}
}

// ─────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────

func extractBearer(r *http.Request) string {
	v := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(v, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

func (h *Handler) handleRouterError(w http.ResponseWriter, err error) {
	re := h.classifyError(err)
	h.writeError(w, re.status, re.code, err.Error())
}

type routerError struct {
	status int
	code   string
}

func (h *Handler) classifyError(err error) routerError {
	// Check for ProviderError first
	var provErr *provider.ProviderError
	if errors.As(err, &provErr) {
		switch provErr.Type {
		case provider.ErrorTypeRateLimit:
			return routerError{http.StatusBadGateway, "rate_limit"}
		case provider.ErrorTypeQuotaExceeded:
			return routerError{http.StatusBadGateway, "quota_exceeded"}
		case provider.ErrorTypeAuth:
			return routerError{http.StatusUnauthorized, "auth_error"}
		case provider.ErrorTypeTimeout:
			return routerError{http.StatusBadGateway, "timeout"}
		case provider.ErrorTypeUpstream:
			return routerError{http.StatusBadGateway, "upstream_error"}
		default:
			return routerError{http.StatusBadGateway, "upstream_error"}
		}
	}

	// Fallback to existing error classification
	switch {
	case errors.Is(err, apierrors.ErrProviderNotFound):
		return routerError{http.StatusBadRequest, "provider_not_found"}
	case errors.Is(err, apierrors.ErrNoCredential):
		return routerError{http.StatusServiceUnavailable, "no_credential"}
	case errors.Is(err, apierrors.ErrModelNotAllowed):
		return routerError{http.StatusForbidden, "model_not_allowed"}
	case errors.Is(err, apierrors.ErrUnauthorized):
		return routerError{http.StatusUnauthorized, "auth_error"}
	default:
		errStr := err.Error()
		if strings.Contains(errStr, "timeout") {
			return routerError{http.StatusBadGateway, "timeout"}
		}
		if strings.Contains(errStr, "rate limit") {
			return routerError{http.StatusBadGateway, "rate_limit"}
		}
		return routerError{http.StatusBadGateway, "upstream_error"}
	}
}

func (h *Handler) writeError(w http.ResponseWriter, status int, code, msg string) {
	h.writeJSON(w, status, models.OpenAIError{
		Error: models.OpenAIErrorBody{
			Message: msg,
			Type:    "error",
			Code:    code,
		},
	})
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.logger.Error("json encode error", "err", err)
	}
}

