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
func countAnthropicTexts(chatRequest *models.ChatCompletionRequest) []string {
	var output []string
	for _, message := range chatRequest.Messages {
		if strings.TrimSpace(message.Content) != "" {
			output = append(output, message.Content)
		}
		for _, part := range message.ContentParts {
			if text := strings.TrimSpace(part.TextContent()); text != "" {
				output = append(output, text)
			}
		}
		for _, toolCall := range message.ToolCalls {
			if toolCall.Function.Name != "" {
				output = append(output, toolCall.Function.Name)
			}
			if strings.TrimSpace(toolCall.Function.Arguments) != "" {
				output = append(output, toolCall.Function.Arguments)
			}
		}
	}
	for _, tool := range chatRequest.Tools {
		if tool.Function != nil && tool.Function.Name != "" {
			output = append(output, tool.Function.Name)
		}
	}
	return output
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
func (h *Handler) anthropicCountTokens(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	var request anthropicRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		h.writeCompatDecodeError(w, r, err)
		return
	}
	if request.Model == "" {
		h.logValidationFailure(r, "", "invalid_request_error")
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", stringPointer("model"))
		return
	}
	if len(request.Messages) == 0 {
		h.logValidationFailure(r, models.ModelId(request.Model), "invalid_request_error")
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'messages'", stringPointer("messages"))
		return
	}
	chatRequest, err := anthropicToOpenAI(&request)
	if err != nil {
		h.logValidationFailure(r, models.ModelId(request.Model), "invalid_request_error")
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", err.Error(), nil)
		return
	}
	if deny := authorizeModel(token, chatRequest.Model); deny != nil {
		h.logDenyFailure(r, chatRequest.Model, deny)
		h.writeCompatError(w, r, deny.status, deny.code, deny.message, deny.parameter)
		return
	}
	echoAnthropicVersion(w, r)
	h.writeJSON(w, http.StatusOK, models.AnthropicTokenCount{
		InputTokens: models.EstimateInputTokens(countAnthropicTexts(chatRequest)),
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
func (h *Handler) anthropicComplete(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	var request models.AnthropicCompleteRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		h.writeCompatDecodeError(w, r, err)
		return
	}
	if request.Model == "" {
		h.logValidationFailure(r, "", "invalid_request_error")
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", stringPointer("model"))
		return
	}
	if request.Prompt == "" {
		h.logValidationFailure(r, models.ModelId(request.Model), "invalid_request_error")
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'prompt'", stringPointer("prompt"))
		return
	}
	if request.MaxTokensToSample <= 0 {
		h.logValidationFailure(r, models.ModelId(request.Model), "invalid_request_error")
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'max_tokens_to_sample'", stringPointer("max_tokens_to_sample"))
		return
	}
	messageRequest := request.ToMessageRequest()
	chatRequest, err := anthropicToOpenAI(messageRequest)
	if err != nil {
		h.logValidationFailure(r, models.ModelId(request.Model), "invalid_request_error")
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", err.Error(), nil)
		return
	}
	if deny := authorizeModel(token, chatRequest.Model); deny != nil {
		h.logDenyFailure(r, chatRequest.Model, deny)
		h.writeCompatError(w, r, deny.status, deny.code, deny.message, deny.parameter)
		return
	}
	if request.Stream {
		h.anthropicStream(w, r, chatRequest, token, start)
		return
	}
	response, err := h.router.Complete(r.Context(), chatRequest, token)
	duration := time.Since(start)
	var usage *models.ChatCompletionUsage
	if err == nil && response != nil {
		usage = &response.Usage
	}
	h.recordRouteMetric(r.Context(), start, chatRequest.Model, token, duration, err, usage)
	if err != nil {
		h.logRouterFailure(r, chatRequest.Model, err)
		h.handleCompatRouterError(w, r, err)
		return
	}
	echoAnthropicVersion(w, r)
	h.writeJSON(w, http.StatusOK, completeFromOpenAI(response, request.Model))
}

// completeFromOpenAI renders a chat completion as a legacy completion:
// first-choice text, stop mapped to stop_sequence|max_tokens.
func completeFromOpenAI(response *models.ChatCompletionResponse, model string) *models.AnthropicCompleteResponse {
	output := &models.AnthropicCompleteResponse{
		ID:         "cmpl_router",
		Type:       "completion",
		StopReason: "stop_sequence",
		Model:      model,
	}
	if len(response.Choices) == 0 {
		return output
	}
	choice := response.Choices[0]
	if response.ID != "" {
		output.ID = response.ID
	}
	output.Completion = choice.Message.Content
	if choice.FinishReason == "length" {
		output.StopReason = "max_tokens"
	}
	return output
}
