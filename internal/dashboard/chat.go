package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

// apiChatCompletions proxies a dashboard-authenticated chat request to the router.
// @Summary      Dashboard chat completions (session auth)
// @Description  Proxies chat completions via the router using the dashboard session. Used by the Chat tab. Supports streaming via SSE.
// @Tags         Chat
// @Accept       json
// @Produce      json
// @Param        request body models.ChatCompletionRequest true "Chat request"
// @Success      200 {object} models.ChatCompletionResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      502 {object} models.OpenAIError
// @Router       /api/llm-router/dashboard/chat/completions [post]
func (h *Handler) apiChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req models.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonErr(w, http.StatusBadRequest, fmt.Sprintf("malformed request body: %s", err))
		return
	}
	// Basic validation — mirror OpenAI requirements
	if string(req.Model) == "" {
		h.jsonErr(w, http.StatusBadRequest, "field 'model' is required")
		return
	}
	if len(req.Messages) == 0 {
		h.jsonErr(w, http.StatusBadRequest, "field 'messages' must contain at least one message")
		return
	}
	// Validate model exists (provider lookup)
	if _, _, err := req.Model.Parse(); err != nil {
		h.jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid model id: %s", err))
		return
	}

	start := time.Now()

	if req.Stream {
		h.handleChatStream(w, r, &req, start)
		return
	}

	resp, err := h.routerSvc.Complete(r.Context(), &req, nil)
	duration := time.Since(start)

	// Metrics — use dashboard as token id
	providerType, _, _ := req.Model.Parse()
	providerID, _ := h.routerSvc.GetProviderIDForModel(r.Context(), req.Model)
	event := models.MetricEvent{
		Timestamp:    start,
		ProviderID:   providerID,
		ProviderType: providerType,
		Model:        req.Model,
		TokenID:      "dashboard",
		Duration:     duration,
		StatusCode:   http.StatusOK,
	}
	if err == nil && resp != nil && resp.Usage.TotalTokens > 0 {
		event.TokensInput = int64(resp.Usage.PromptTokens)
		event.TokensOutput = int64(resp.Usage.CompletionTokens)
	}
	if err != nil {
		re := classifyChatError(err)
		event.StatusCode = re.status
		event.ErrorType = re.code
	}
	h.metricsSvc.RecordRequest(event)

	if err != nil {
		handleChatRouterError(w, err, h)
		return
	}

	h.json(w, http.StatusOK, resp)
}

func (h *Handler) handleChatStream(w http.ResponseWriter, r *http.Request, req *models.ChatCompletionRequest, start time.Time) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.jsonErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	err := h.routerSvc.CompleteStream(r.Context(), req, w, nil)
	duration := time.Since(start)
	providerType, _, _ := req.Model.Parse()
	providerID, _ := h.routerSvc.GetProviderIDForModel(r.Context(), req.Model)
	event := models.MetricEvent{
		Timestamp:    start,
		ProviderID:   providerID,
		ProviderType: providerType,
		Model:        req.Model,
		TokenID:      "dashboard",
		Duration:     duration,
		StatusCode:   http.StatusOK,
	}
	if err != nil {
		event.StatusCode = http.StatusInternalServerError
		re := classifyChatError(err)
		event.ErrorType = re.code
		h.logger.Error("chat stream error", "err", err)
		// Send error as SSE event for frontend
		errorObj := models.OpenAIError{
			Error: models.OpenAIErrorBody{
				Message: err.Error(),
				Type:    "error",
				Code:    re.code,
			},
		}
		if b, e := json.Marshal(errorObj); e == nil {
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
	}
	h.metricsSvc.RecordRequest(event)
}

func handleChatRouterError(w http.ResponseWriter, err error, h *Handler) {
	re := classifyChatError(err)
	h.json(w, re.status, models.OpenAIError{
		Error: models.OpenAIErrorBody{
			Message: err.Error(),
			Type:    "error",
			Code:    re.code,
		},
	})
}

type chatRouterError struct {
	status int
	code   string
}

func classifyChatError(err error) chatRouterError {
	var provErr *provider.ProviderError
	if errors.As(err, &provErr) {
		switch provErr.Type {
		case provider.ErrorTypeRateLimit:
			return chatRouterError{http.StatusBadGateway, "rate_limit"}
		case provider.ErrorTypeQuotaExceeded:
			return chatRouterError{http.StatusBadGateway, "quota_exceeded"}
		case provider.ErrorTypeAuth:
			return chatRouterError{http.StatusUnauthorized, "auth_error"}
		case provider.ErrorTypeTimeout:
			return chatRouterError{http.StatusBadGateway, "timeout"}
		case provider.ErrorTypeUpstream:
			return chatRouterError{http.StatusBadGateway, "upstream_error"}
		default:
			return chatRouterError{http.StatusBadGateway, "upstream_error"}
		}
	}
	switch {
	case errors.Is(err, apierrors.ErrProviderNotFound):
		return chatRouterError{http.StatusBadRequest, "provider_not_found"}
	case errors.Is(err, apierrors.ErrNoCredential):
		return chatRouterError{http.StatusServiceUnavailable, "no_credential"}
	case errors.Is(err, apierrors.ErrModelNotAllowed):
		return chatRouterError{http.StatusForbidden, "model_not_allowed"}
	case errors.Is(err, apierrors.ErrProviderNotAllowed):
		return chatRouterError{http.StatusForbidden, "provider_not_allowed"}
	case errors.Is(err, apierrors.ErrCredentialNotAllowed):
		return chatRouterError{http.StatusForbidden, "credential_not_allowed"}
	case errors.Is(err, apierrors.ErrUnauthorized):
		return chatRouterError{http.StatusUnauthorized, "auth_error"}
	default:
		s := err.Error()
		if strings.Contains(s, "timeout") {
			return chatRouterError{http.StatusBadGateway, "timeout"}
		}
		if strings.Contains(s, "rate limit") {
			return chatRouterError{http.StatusBadGateway, "rate_limit"}
		}
		return chatRouterError{http.StatusBadGateway, "upstream_error"}
	}
}
