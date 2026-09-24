package v1

import (
	"context"
	"net/http"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// recordRouteMetric builds and records the shared MetricEvent for one routed
// non-streaming request. Chat usage attaches only when provided.
func (h *Handler) recordRouteMetric(ctx context.Context, start time.Time, model models.ModelId, t *models.RouterToken, duration time.Duration, err error, usage *models.ChatCompletionUsage) {
	providerType, _, _ := model.Parse()
	providerID, _ := h.router.GetProviderIDForModel(ctx, model)
	tokenID := ""
	if t != nil {
		tokenID = t.ID
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
		re := h.classifyError(err)
		event.StatusCode = re.status
		event.ErrorType = re.code
	}
	h.metrics.RecordRequest(event)
}
