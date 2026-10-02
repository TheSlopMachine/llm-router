package v1

// Anthropic Messages API adapter (POST /v1/messages).
//
// The endpoint converts the Anthropic request into the OpenAI chat shape at
// the edge, routes it through the normal pipeline (Complete/CompleteStream),
// and converts the answer back into the Anthropic message shape. Streaming
// translates OpenAI SSE chunks into Anthropic events (see
// anthropic_stream.go). Plugins never see the Anthropic protocol.
//
// Conversion lives in the models package (ToChat, AnthropicMessageFromChat,
// AnthropicFinishReason) so the router reuses it for message batches and
// legacy completions; this file keeps edge aliases plus thin wrappers.
import (
	"encoding/json"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// anthropicRequest is the POST /v1/messages body.
type anthropicRequest = models.AnthropicMessageRequest

type anthropicTool = models.AnthropicTool

type anthropicChoice = models.AnthropicToolChoice

// anthropicBlock is one content block inside a message or a tool_result.
type anthropicBlock = models.AnthropicContentBlock

type anthropicSource = models.AnthropicContentSource

// anthropicResponse is the non-stream /v1/messages answer.
type anthropicResponse = models.AnthropicMessage

type anthropicUsage = models.AnthropicUsage

// anthropicFinish maps OpenAI finish reasons to Anthropic stop reasons.
func anthropicFinish(reason string) string {
	return models.AnthropicFinishReason(reason)
}

// anthropicBlocks parses a polymorphic Anthropic content field (plain string
// or block array) into blocks.
func anthropicBlocks(raw json.RawMessage) ([]anthropicBlock, error) {
	return models.AnthropicBlocks(raw)
}

// anthropicToOpenAI converts the Anthropic request into the router's chat
// shape. Document blocks are refused: the chat pipeline has no document
// part. Cache-control, top_k and top-level thinking budgets are accepted
// and documented; they carry no mapping.
func anthropicToOpenAI(r *anthropicRequest) (*models.ChatCompletionRequest, error) {
	return r.ToChat()
}

// anthropicFromOpenAI renders the chat completion as an Anthropic message.
func anthropicFromOpenAI(resp *models.ChatCompletionResponse, model string) *anthropicResponse {
	return models.AnthropicMessageFromChat(resp, model)
}
