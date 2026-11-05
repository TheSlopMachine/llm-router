package v1

import (
	"bytes"
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
// Metrics only: no logs emit here; callers log validation, deny and router
// outcomes at the edge with endpoint, code, model, provider_id and error.
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
// request that committed (status 200 already sent). A mid-stream failure
// records the classified status and code, the same mapping as unary
// requests. usage carries the token counts seen in the stream (nil when the
// upstream sent none).
func BuildStreamMetricEvent(start time.Time, model models.ModelId, providerID, tokenID string, duration time.Duration, streamErr error, usage *models.ChatCompletionUsage) models.MetricEvent {
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
	if usage != nil {
		event.TokensInput = int64(usage.PromptTokens)
		event.TokensOutput = int64(usage.CompletionTokens)
	}
	if streamErr != nil {
		apiError := apierrors.ToAPIError(streamErr)
		event.StatusCode = apiError.Status
		event.ErrorType = apiError.Code
	}
	return event
}

// MarshalOpenAIStreamError renders the OpenAI-shaped error envelope sent as
// an SSE data frame when a stream fails mid-flight. The message follows the
// edge contract (WireMessage): plugin messages verbatim, never a wrapped Go
// error chain. Callers own the write and flush so the Anthropic path
// (translatedWriter.emit) stays separate.
func MarshalOpenAIStreamError(streamErr error) ([]byte, error) {
	code := apierrors.ToAPIError(streamErr).Code
	return json.Marshal(models.OpenAIError{
		Error: models.OpenAIErrorBody{
			Message: WireMessage(streamErr),
			Type:    apierrors.ErrorTypeForCode(code),
			Code:    code,
		},
	})
}

// StreamMetricsTap forwards OpenAI SSE bytes unchanged and keeps what
// metrics need from the data frames: the last usage object and the model
// that answered (the router stamps it on every frame; a virtual request
// carries its member). Partial frames stay buffered until their newline.
type StreamMetricsTap struct {
	next    io.Writer
	flusher interface{ Flush() }
	pending []byte
	usage   *models.ChatCompletionUsage
	model   models.ModelId
}

// NewStreamMetricsTap wraps next; flusher receives forwarded Flush calls.
func NewStreamMetricsTap(next io.Writer, flusher interface{ Flush() }) *StreamMetricsTap {
	return &StreamMetricsTap{next: next, flusher: flusher}
}

// maxPendingFrame bounds the buffered partial line: a frame longer than
// this is passed through without inspection.
const maxPendingFrame = 1 << 20

func (t *StreamMetricsTap) Write(p []byte) (int, error) {
	t.pending = append(t.pending, p...)
	for {
		newline := bytes.IndexByte(t.pending, '\n')
		if newline < 0 {
			break
		}
		t.inspect(t.pending[:newline])
		t.pending = t.pending[newline+1:]
	}
	if len(t.pending) > maxPendingFrame {
		t.pending = t.pending[:0]
	}
	return t.next.Write(p)
}

// Flush forwards to the downstream flusher.
func (t *StreamMetricsTap) Flush() {
	if t.flusher != nil {
		t.flusher.Flush()
	}
}

// Usage returns the last usage object seen, or nil.
func (t *StreamMetricsTap) Usage() *models.ChatCompletionUsage { return t.usage }

// Model returns the answering model from the first frame that named one,
// or requested when none did.
func (t *StreamMetricsTap) Model(requested models.ModelId) models.ModelId {
	if t.model != "" {
		return t.model
	}
	return requested
}

func (t *StreamMetricsTap) inspect(line []byte) {
	payload, found := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:"))
	if !found {
		return
	}
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 || payload[0] != '{' {
		return
	}
	needsModel := t.model == ""
	needsUsage := bytes.Contains(payload, []byte(`"usage"`))
	if !needsModel && !needsUsage {
		return
	}
	var chunk struct {
		Model string                      `json:"model"`
		Usage *models.ChatCompletionUsage `json:"usage"`
	}
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return
	}
	if needsModel && chunk.Model != "" {
		t.model = models.ModelId(chunk.Model)
	}
	if chunk.Usage != nil {
		t.usage = chunk.Usage
	}
}

// answeredModel is the model a unary chat response names (stamped by the
// router), or requested when the request failed or the response names none.
// Metrics attribute the request to it, so a virtual request counts against
// the member that answered.
func answeredModel(requested models.ModelId, response *models.ChatCompletionResponse) models.ModelId {
	if response != nil && response.Model != "" {
		return models.ModelId(response.Model)
	}
	return requested
}

// WriteOpenAIStreamError marshals the error envelope and emits it as an SSE
// data frame. It shares the exact write sequence of the OpenAI streaming
// paths; the dashboard path keeps its own guarded write on top of
// MarshalOpenAIStreamError.
func WriteOpenAIStreamError(w io.Writer, flusher interface{ Flush() }, streamErr error) {
	payload, _ := MarshalOpenAIStreamError(streamErr)
	fmt.Fprintf(w, "data: %s\n\n", payload)
	flusher.Flush()
}
