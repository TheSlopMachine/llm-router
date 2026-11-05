package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	v1 "github.com/TheSlopMachine/llm-router/internal/api/v1"
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

	// Metrics use "dashboard" as token id and the answering model: a
	// virtual request counts against the member that answered.
	answered := request.Model
	if err == nil && response != nil && response.Model != "" {
		answered = models.ModelId(response.Model)
	}
	providerType, _, _ := answered.Parse()
	providerID, _ := h.routerService.GetProviderIDForModel(r.Context(), answered)
	event := models.MetricEvent{
		Timestamp:    start,
		ProviderID:   providerID,
		ProviderType: providerType,
		Model:        answered,
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
		h.logError("Chat completion failed", "model", string(request.Model), "provider_id", providerID, "error", err)
		handleChatRouterError(w, err, h)
		return
	}

	h.json(w, http.StatusOK, response)
}

func (h *Handler) handleChatStream(w http.ResponseWriter, r *http.Request, request *models.ChatCompletionRequest, start time.Time) {
	stream, ok := httpkit.NewStreamWriter(w)
	if !ok {
		h.logError("Streaming not supported", "model", string(request.Model))
		h.jsonErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	tap := v1.NewStreamMetricsTap(stream, stream)
	err := h.routerService.CompleteStream(r.Context(), request, tap, nil)
	duration := time.Since(start)
	providerID, _ := h.routerService.GetProviderIDForModel(r.Context(), request.Model)
	if err != nil {
		h.logError("Chat stream failed", "model", string(request.Model), "provider_id", providerID, "error", err)
	}
	if err != nil && !stream.Started() {
		event := v1.BuildStreamMetricEvent(start, request.Model, providerID, "dashboard", duration, err, nil)
		h.metricsService.RecordRequest(event)
		handleChatRouterError(w, err, h)
		return
	}
	if err != nil {
		v1.WriteOpenAIStreamError(stream, stream, err)
	}
	answered := tap.Model(request.Model)
	answeredProviderID, _ := h.routerService.GetProviderIDForModel(r.Context(), answered)
	h.metricsService.RecordRequest(v1.BuildStreamMetricEvent(start, answered, answeredProviderID, "dashboard", duration, err, tap.Usage()))
}

func handleChatRouterError(w http.ResponseWriter, err error, h *Handler) {
	routerError := classifyChatError(err)
	h.json(w, routerError.status, models.OpenAIError{
		Error: models.OpenAIErrorBody{
			Message: v1.WireMessage(err),
			Type:    apierrors.ErrorTypeForCode(routerError.code),
			Code:    routerError.code,
		},
	})
}

type chatRouterError struct {
	status int
	code   string
}

func classifyChatError(err error) chatRouterError {
	routerError := apierrors.ToAPIError(err)
	return chatRouterError{routerError.Status, routerError.Code}
}
