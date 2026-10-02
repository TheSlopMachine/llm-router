package v1

import (
	"net/http"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// anthropicCountTokens handles POST /v1/messages/count_tokens: same body as
// messages minus output controls, answered with the documented token
// heuristic (4 chars per token). No upstream call runs.
func countAnthropicTexts(chatReq *models.ChatCompletionRequest) []string {
	var out []string
	for _, m := range chatReq.Messages {
		if strings.TrimSpace(m.Content) != "" {
			out = append(out, m.Content)
		}
		for _, p := range m.ContentParts {
			if t := strings.TrimSpace(p.TextContent()); t != "" {
				out = append(out, t)
			}
		}
		for _, tc := range m.ToolCalls {
			if tc.Function.Name != "" {
				out = append(out, tc.Function.Name)
			}
			if strings.TrimSpace(tc.Function.Arguments) != "" {
				out = append(out, tc.Function.Arguments)
			}
		}
	}
	for _, t := range chatReq.Tools {
		if t.Function != nil && t.Function.Name != "" {
			out = append(out, t.Function.Name)
		}
	}
	return out
}

// anthropicCountTokens handles POST /v1/messages/count_tokens
// @Summary      Count message tokens (Anthropic)
// @Description  Estimates input tokens for a messages request with the documented
// @Description  4-chars-per-token heuristic. Same body as POST /v1/messages; no
// @Description  upstream call runs. Errors use the negotiated envelope.
// @Tags         Anthropic API
// @Accept       json
// @Produce      json
// @Param        request body models.AnthropicMessageRequest true "Message request to count"
// @Param        anthropic-version header string false "Anthropic API version (accepted, never pinned)"
// @Param        anthropic-beta header string false "Anthropic beta flags (passthrough)"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.AnthropicTokenCount "Token count"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      403 {object} models.OpenAIError "Forbidden - model not allowed by token rules"
// @Router       /v1/messages/count_tokens [post]
// @Security     BearerAuth
func (h *Handler) anthropicCountTokens(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	var req anthropicRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeCompatDecodeError(w, r, err)
		return
	}
	if req.Model == "" {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", strPtr("model"))
		return
	}
	if len(req.Messages) == 0 {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'messages'", strPtr("messages"))
		return
	}
	chatReq, err := anthropicToOpenAI(&req)
	if err != nil {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", err.Error(), nil)
		return
	}
	if deny := authorizeModel(t, chatReq.Model); deny != nil {
		h.writeCompatError(w, r, deny.status, deny.code, deny.msg, deny.param)
		return
	}
	echoAnthropicVersion(w, r)
	h.writeJSON(w, http.StatusOK, models.AnthropicTokenCount{
		InputTokens: models.EstimateInputTokens(countAnthropicTexts(chatReq)),
	})
}

// anthropicComplete handles POST /v1/complete: legacy Anthropic text
// completions, first-class. The prompt wraps as a single-turn messages
// request through the normal pipeline.
// @Summary      Create text completion (Anthropic legacy)
// @Description  Legacy Anthropic text completions. The prompt wraps as a single-turn
// @Description  messages request; the answer renders as {type:completion}. Streams
// @Description  with Anthropic events when stream is true. Errors use the
// @Description  negotiated envelope.
// @Tags         Anthropic API
// @Accept       json
// @Produce      json
// @Param        request body models.AnthropicCompleteRequest true "Completion request"
// @Param        anthropic-version header string false "Anthropic API version (accepted, never pinned)"
// @Param        anthropic-beta header string false "Anthropic beta flags (passthrough)"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.AnthropicCompleteResponse "Completion"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      403 {object} models.OpenAIError "Forbidden - model not allowed by token rules"
// @Failure      502 {object} models.OpenAIError "Bad Gateway - upstream provider error"
// @Failure      503 {object} models.OpenAIError "Service Unavailable - no credential available"
// @Router       /v1/complete [post]
// @Security     BearerAuth
func (h *Handler) anthropicComplete(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	var req models.AnthropicCompleteRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeCompatDecodeError(w, r, err)
		return
	}
	if req.Model == "" {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", strPtr("model"))
		return
	}
	if req.Prompt == "" {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'prompt'", strPtr("prompt"))
		return
	}
	if req.MaxTokensToSample <= 0 {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'max_tokens_to_sample'", strPtr("max_tokens_to_sample"))
		return
	}
	msgReq := req.ToMessageRequest()
	chatReq, err := anthropicToOpenAI(msgReq)
	if err != nil {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", err.Error(), nil)
		return
	}
	if deny := authorizeModel(t, chatReq.Model); deny != nil {
		h.writeCompatError(w, r, deny.status, deny.code, deny.msg, deny.param)
		return
	}
	if req.Stream {
		h.anthropicStream(w, r, chatReq, t, start)
		return
	}
	resp, err := h.router.Complete(r.Context(), chatReq, t)
	duration := time.Since(start)
	var usage *models.ChatCompletionUsage
	if err == nil && resp != nil {
		usage = &resp.Usage
	}
	h.recordRouteMetric(r.Context(), start, chatReq.Model, t, duration, err, usage)
	if err != nil {
		h.handleCompatRouterError(w, r, err)
		return
	}
	echoAnthropicVersion(w, r)
	h.writeJSON(w, http.StatusOK, completeFromOpenAI(resp, req.Model))
}

// completeFromOpenAI renders a chat completion as a legacy completion:
// first-choice text, stop mapped to stop_sequence|max_tokens.
func completeFromOpenAI(resp *models.ChatCompletionResponse, model string) *models.AnthropicCompleteResponse {
	out := &models.AnthropicCompleteResponse{
		ID:         "cmpl_router",
		Type:       "completion",
		StopReason: "stop_sequence",
		Model:      model,
	}
	if len(resp.Choices) == 0 {
		return out
	}
	choice := resp.Choices[0]
	if resp.ID != "" {
		out.ID = resp.ID
	}
	out.Completion = choice.Message.Content
	if choice.FinishReason == "length" {
		out.StopReason = "max_tokens"
	}
	return out
}
