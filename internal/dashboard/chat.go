package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/httpkit"
	"github.com/TheSlopMachine/llm-router/internal/models"
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
	var request models.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.jsonErr(w, http.StatusBadRequest, fmt.Sprintf("malformed request body: %s", err))
		return
	}
	// Basic validation — mirror OpenAI requirements
	if string(request.Model) == "" {
		h.jsonErr(w, http.StatusBadRequest, "field 'model' is required")
		return
	}
	if len(request.Messages) == 0 {
		h.jsonErr(w, http.StatusBadRequest, "field 'messages' must contain at least one message")
		return
	}
	// Validate model exists (provider lookup)
	if _, _, err := request.Model.Parse(); err != nil {
		h.jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid model id: %s", err))
		return
	}

	start := time.Now()

	if request.Stream {
		h.handleChatStream(w, r, &request, start)
		return
	}

	response, err := h.routerService.Complete(r.Context(), &request, nil)
	duration := time.Since(start)

	// Metrics — use dashboard as token id
	providerType, _, _ := request.Model.Parse()
	providerID, _ := h.routerService.GetProviderIDForModel(r.Context(), request.Model)
	event := models.MetricEvent{
		Timestamp:    start,
		ProviderID:   providerID,
		ProviderType: providerType,
		Model:        request.Model,
		TokenID:      "dashboard",
		Duration:     duration,
		StatusCode:   http.StatusOK,
	}
	if err == nil && response != nil && response.Usage.TotalTokens > 0 {
		event.TokensInput = int64(response.Usage.PromptTokens)
		event.TokensOutput = int64(response.Usage.CompletionTokens)
	}
	if err != nil {
		routerError := classifyChatError(err)
		event.StatusCode = routerError.status
		event.ErrorType = routerError.code
	}
	h.metricsService.RecordRequest(event)

	if err != nil {
		handleChatRouterError(w, err, h)
		return
	}

	h.json(w, http.StatusOK, response)
}

func (h *Handler) handleChatStream(w http.ResponseWriter, r *http.Request, request *models.ChatCompletionRequest, start time.Time) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.jsonErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	httpkit.WriteSSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	err := h.routerService.CompleteStream(r.Context(), request, w, nil)
	duration := time.Since(start)
	providerType, _, _ := request.Model.Parse()
	providerID, _ := h.routerService.GetProviderIDForModel(r.Context(), request.Model)
	event := models.MetricEvent{
		Timestamp:    start,
		ProviderID:   providerID,
		ProviderType: providerType,
		Model:        request.Model,
		TokenID:      "dashboard",
		Duration:     duration,
		StatusCode:   http.StatusOK,
	}
	if err != nil {
		event.StatusCode = http.StatusInternalServerError
		routerError := classifyChatError(err)
		event.ErrorType = routerError.code
		h.logger.Error("chat stream error", "err", err)
		// Send error as SSE event for frontend
		errorResponse := models.OpenAIError{
			Error: models.OpenAIErrorBody{
				Message: err.Error(),
				Type:    apierrors.ErrorTypeForCode(routerError.code),
				Code:    routerError.code,
			},
		}
		if errorBytes, e := json.Marshal(errorResponse); e == nil {
			fmt.Fprintf(w, "data: %s\n\n", errorBytes)
			flusher.Flush()
		}
	}
	h.metricsService.RecordRequest(event)
}

func handleChatRouterError(w http.ResponseWriter, err error, h *Handler) {
	routerError := classifyChatError(err)
	h.json(w, routerError.status, models.OpenAIError{
		Error: models.OpenAIErrorBody{
			Message: chatWireMessage(err),
			Type:    apierrors.ErrorTypeForCode(routerError.code),
			Code:    routerError.code,
		},
	})
}

// chatWireMessage renders the raw plugin message for terminal provider
// errors, the Go error otherwise.
func chatWireMessage(err error) string {
	var providerError *models.ProviderError
	if errors.As(err, &providerError) && providerError.Message != "" {
		return providerError.Message
	}
	return err.Error()
}

type chatRouterError struct {
	status int
	code   string
}

func classifyChatError(err error) chatRouterError {
	routerError := apierrors.ToAPIError(err)
	return chatRouterError{routerError.Status, routerError.Code}
}
