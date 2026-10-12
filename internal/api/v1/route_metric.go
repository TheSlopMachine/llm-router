package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// recordRouteMetric builds and records the shared MetricEvent for one routed
// non-streaming request. Chat usage attaches only when provided.
func (h *Handler) recordRouteMetric(ctx context.Context, start time.Time, model models.ModelId, token *models.RouterToken, duration time.Duration, err error, usage *models.ChatCompletionUsage) {
	providerType, _, _ := model.Parse()
	providerID, _ := h.router.GetProviderIDForModel(ctx, model)
	tokenID := ""
	if token != nil {
		tokenID = token.ID
	}
	event := models.MetricEvent{
		Timestamp:    start,
		ProviderID:   providerID,
		ProviderType: providerType,
		Model:        model,
		TokenID:      tokenID,
		Duration:     duration,
		StatusCode:   http.StatusOK,
	}
	if err == nil && usage != nil && usage.TotalTokens > 0 {
		event.TokensInput = int64(usage.PromptTokens)
		event.TokensOutput = int64(usage.CompletionTokens)
	}
	if err != nil {
		routerError := h.classifyError(err)
		event.StatusCode = routerError.status
		event.ErrorType = routerError.code
	}
	h.metrics.RecordRequest(event)
}

// BuildStreamMetricEvent builds the shared MetricEvent for one streaming
// request. Streams hardcode 500 on failure: headers already went out with
// 200, so the mapped status never reaches the client. Non-streaming
// recordRouteMetric maps the status instead; the two semantics stay separate.
func BuildStreamMetricEvent(start time.Time, model models.ModelId, providerID, tokenID string, duration time.Duration, streamErr error) models.MetricEvent {
	providerType, _, _ := model.Parse()
	event := models.MetricEvent{
		Timestamp:    start,
		ProviderID:   providerID,
		ProviderType: providerType,
		Model:        model,
		TokenID:      tokenID,
		Duration:     duration,
		StatusCode:   http.StatusOK,
	}
	if streamErr != nil {
		event.StatusCode = http.StatusInternalServerError
		event.ErrorType = apierrors.ToAPIError(streamErr).Code
	}
	return event
}

// MarshalOpenAIStreamError renders the OpenAI-shaped error envelope sent as
// an SSE data frame when a stream fails mid-flight. Callers own the write
// and flush so the Anthropic path (translatedWriter.emit) stays separate.
func MarshalOpenAIStreamError(streamErr error) ([]byte, error) {
	code := apierrors.ToAPIError(streamErr).Code
	return json.Marshal(models.OpenAIError{
		Error: models.OpenAIErrorBody{
			Message: streamErr.Error(),
			Type:    apierrors.ErrorTypeForCode(code),
			Code:    code,
		},
	})
}

// WriteOpenAIStreamError marshals the error envelope and emits it as an SSE
// data frame. It shares the exact write sequence of the OpenAI streaming
// paths; the dashboard path keeps its own guarded write on top of
// MarshalOpenAIStreamError.
func WriteOpenAIStreamError(w io.Writer, flusher http.Flusher, streamErr error) {
	payload, _ := MarshalOpenAIStreamError(streamErr)
	fmt.Fprintf(w, "data: %s\n\n", payload)
	flusher.Flush()
}
