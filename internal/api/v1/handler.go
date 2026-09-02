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
	"sort"
	"strings"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/agent"
	"github.com/TheSlopMachine/llm-router/internal/services/metrics"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
)

// Handler holds the dependencies for the v1 API.
type Handler struct {
	tokens       *token.Service
	router       *router.Service
	metrics      *metrics.Service
	providerSvc  *provider.Service
	modelInfoSvc *modelinfo.Service
	agentSvc     *agent.Service
	logger       *slog.Logger
}

// New constructs a v1 Handler.
func New(tokens *token.Service, routerSvc *router.Service, metricsSvc *metrics.Service, providerSvc *provider.Service, modelInfoSvc *modelinfo.Service, agentSvc *agent.Service, logger *slog.Logger) *Handler {
	return &Handler{
		tokens:       tokens,
		router:       routerSvc,
		metrics:      metricsSvc,
		providerSvc:  providerSvc,
		modelInfoSvc: modelInfoSvc,
		agentSvc:     agentSvc,
		logger:       logger,
	}
}

// Register mounts all /v1 routes onto mux.
// Safe endpoints (GET /v1/models) are whitelisted to allow anonymous access.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/chat/completions", h.auth(h.chatCompletions, false))
	mux.HandleFunc("GET /v1/models", h.auth(h.listModels, true))
	mux.HandleFunc("HEAD /v1/models", h.auth(h.listModels, true))
	mux.HandleFunc("OPTIONS /v1/models", h.auth(h.listModels, true))
	mux.HandleFunc("GET /v1/models/{model}", h.auth(h.retrieveModel, true))
	mux.HandleFunc("HEAD /v1/models/{model}", h.auth(h.retrieveModel, true))
	mux.HandleFunc("OPTIONS /v1/models/{model}", h.auth(h.retrieveModel, true))
	// Fallback for unknown paths - always JSON, never SPA/redirect
	// Also handles slashed ModelIds like kiro/claude-haiku-4.5 via notFound delegation
	mux.HandleFunc("/", h.notFoundWithModelFallback)
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
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("malformed request body: %s", err), nil)
		return
	}
	if req.Model == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", strPtr("model"))
		return
	}
	if len(req.Messages) == 0 {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'messages'", strPtr("messages"))
		return
	}

	if t != nil && !t.Rules.Allows(req.Model) {
		h.writeError(w, http.StatusForbidden, "model_not_allowed",
			fmt.Sprintf("model %q is not allowed by your token's rules", req.Model), strPtr("model"))
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
	tokenID := ""
	if t != nil {
		tokenID = t.ID
	}
	event := models.MetricEvent{
		Timestamp:    start,
		ProviderID:   providerID,
		ProviderType: providerType,
		Model:        req.Model,
		TokenID:      tokenID,
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
		h.writeError(w, http.StatusInternalServerError, "server_error", "streaming is not supported by this server", nil)
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
	tokenID := ""
	if t != nil {
		tokenID = t.ID
	}
	event := models.MetricEvent{
		Timestamp:    start,
		ProviderID:   providerID,
		ProviderType: providerType,
		Model:        req.Model,
		TokenID:      tokenID,
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
				Type:    errorTypeForCode(re.code),
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

// listModels handles GET /v1/models - global, independent of token rules
func (h *Handler) listModels(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	type modelEntry struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}
	type modelList struct {
		Object string       `json:"object"`
		Data   []modelEntry `json:"data"`
	}

	var entries []modelEntry
	// Global listing like dashboard Available Models, not per-token
	if h.providerSvc != nil && h.modelInfoSvc != nil {
		if providers, err := h.providerSvc.List(); err == nil {
			for _, p := range providers {
				if p.Type == "agents" {
					continue
				}
				infos, err := h.modelInfoSvc.GetModelInfos(r.Context(), p.ID)
				if err != nil || len(infos) == 0 {
					continue
				}
				for _, mi := range infos {
					fullID := p.ID + "/" + mi.Name
					entries = append(entries, modelEntry{
						ID:      fullID,
						Object:  "model",
						Created: time.Now().Unix(),
						OwnedBy: p.Type,
					})
				}
			}
		}
	}
	if h.agentSvc != nil {
		if agents, err := h.agentSvc.List(); err == nil {
			for _, a := range agents {
				if a.IsDraft {
					continue
				}
				entries = append(entries, modelEntry{
					ID:      "agents/" + a.ID,
					Object:  "model",
					Created: a.CreatedAt.Unix(),
					OwnedBy: "agents",
				})
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	if entries == nil {
		entries = []modelEntry{}
	}

	h.writeJSON(w, http.StatusOK, modelList{Object: "list", Data: entries})
}

// retrieveModel handles GET /v1/models/{model} - global
func (h *Handler) retrieveModel(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	modelID := strings.TrimPrefix(r.URL.Path, "/v1/models/")
	if modelID == "" || modelID == r.URL.Path {
		if v := r.PathValue("model"); v != "" {
			modelID = v
		}
	}
	modelID = strings.TrimSuffix(modelID, "/")
	if modelID == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "model id is required", strPtr("model"))
		return
	}
	mid := models.ModelId(modelID)
	providerID, modelName, err := mid.Parse()
	if err != nil || providerID == "" || modelName == "" {
		h.writeError(w, http.StatusNotFound, "invalid_request_error", fmt.Sprintf("The model '%s' does not exist", modelID), nil)
		return
	}
	// Check global existence via modelInfo or agents
	exists := false
	var ownedBy string
	if providerID == "agents" && h.agentSvc != nil {
		if _, err := h.agentSvc.Get(modelName); err == nil {
			exists = true
			ownedBy = "agents"
		}
	} else if h.providerSvc != nil && h.modelInfoSvc != nil {
		if _, err := h.providerSvc.Get(providerID); err == nil {
			if _, err := h.modelInfoSvc.GetModelInfo(r.Context(), mid); err == nil {
				exists = true
				if p, err := h.providerSvc.Get(providerID); err == nil {
					ownedBy = p.Type
				} else {
					ownedBy = providerID
				}
			}
		}
	}
	// Fallback: if we can't verify (e.g. no credential), synthesize if provider parsed - for compatibility
	if !exists {
		// If provider exists but model unknown, still 404; if provider unknown, 404
		h.writeError(w, http.StatusNotFound, "invalid_request_error", fmt.Sprintf("The model '%s' does not exist", modelID), nil)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"id":       modelID,
		"object":   "model",
		"created":  time.Now().Unix(),
		"owned_by": ownedBy,
	})
}

func (h *Handler) notFound(w http.ResponseWriter, r *http.Request) {
	h.writeError(w, http.StatusNotFound, "invalid_request_error", fmt.Sprintf("Invalid URL (%s %s)", r.Method, r.URL.Path), nil)
}

func (h *Handler) notFoundWithModelFallback(w http.ResponseWriter, r *http.Request) {
	// Handle slashed ModelIds like kiro/claude-haiku-4.5 which don't match {model} pattern
	if strings.HasPrefix(r.URL.Path, "/v1/models/") && r.URL.Path != "/v1/models/" {
		// Delegate to retrieveModel with same auth logic (safe whitelist applies)
		h.auth(h.retrieveModel, true)(w, r)
		return
	}
	h.notFound(w, r)
}

// ─────────────────────────────────────────────
// Middleware
// ─────────────────────────────────────────────

type authedHandler func(w http.ResponseWriter, r *http.Request, t *models.RouterToken)

// whitelist for safe anonymous access: method -> path -> allow without token
var safeMethods = map[string]bool{
	"GET":     true,
	"HEAD":    true,
	"OPTIONS": true,
}

func isSafeWithoutAuth(r *http.Request) bool {
	if !safeMethods[r.Method] {
		return false
	}
	if r.URL.Path == "/v1/models" {
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/v1/models/") {
		return true
	}
	return false
}

// auth extracts and validates the Bearer token. Safe endpoints allow anonymous access.
func (h *Handler) auth(next authedHandler, allowAnonymous bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := extractBearer(r)

		// Whitelisted safe paths allow missing token
		if raw == "" {
			if allowAnonymous || isSafeWithoutAuth(r) {
				// Anonymous access - pass nil token (handlers must handle nil)
				next(w, r, nil)
				return
			}
			h.writeError(w, http.StatusUnauthorized, "invalid_request_error", "You didn't provide an API key. You need to provide your API key in an Authorization header using Bearer auth.", strPtr("Authorization"))
			return
		}

		t, err := h.tokens.Validate(raw)
		if err != nil {
			if errors.Is(err, apierrors.ErrUnauthorized) {
				h.writeError(w, http.StatusUnauthorized, "invalid_request_error", "Incorrect API key provided: "+raw+". You can find your API key at https://platform.openai.com/account/api-keys.", nil)
				return
			}
			h.writeError(w, http.StatusInternalServerError, "server_error", "token validation failed", nil)
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
	h.writeError(w, re.status, re.code, err.Error(), nil)
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

func (h *Handler) writeError(w http.ResponseWriter, status int, code, msg string, param *string) {
	// Map internal code to OpenAI error type
	errType := errorTypeForCode(code)
	h.writeJSON(w, status, models.OpenAIError{
		Error: models.OpenAIErrorBody{
			Message: msg,
			Type:    errType,
			Param:   param,
			Code:    code,
		},
	})
}

func strPtr(s string) *string { return &s }

func errorTypeForCode(code string) string {
	switch code {
	case "invalid_request_error", "not_found", "model_not_allowed", "provider_not_found":
		return "invalid_request_error"
	case "missing_token", "invalid_token", "auth_error":
		// OpenAI 401 is typically invalid_request_error as well, but keep authentication_error for clarity
		return "invalid_request_error"
	case "rate_limit", "quota_exceeded":
		return "rate_limit_error"
	case "server_error", "upstream_error", "timeout", "internal_error":
		return "server_error"
	default:
		return "invalid_request_error"
	}
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.logger.Error("json encode error", "err", err)
	}
}

