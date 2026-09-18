package v1

// Anthropic Messages API adapter (POST /v1/messages).
//
// The endpoint converts the Anthropic request into the OpenAI chat shape at
// the edge, routes it through the normal pipeline (Complete/CompleteStream),
// and converts the answer back into the Anthropic message shape. Streaming
// translates OpenAI SSE chunks into Anthropic events (see
// anthropic_stream.go). Plugins never see the Anthropic protocol.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// anthropicRequest is the POST /v1/messages body.
type anthropicRequest struct {
	Model         string            `json:"model"`
	MaxTokens     int               `json:"max_tokens"`
	System        json.RawMessage   `json:"system,omitempty"`
	Messages      []json.RawMessage `json:"messages"`
	Tools         []anthropicTool   `json:"tools,omitempty"`
	ToolChoice    *anthropicChoice  `json:"tool_choice,omitempty"`
	Temperature   *float64          `json:"temperature,omitempty"`
	TopP          *float64          `json:"top_p,omitempty"`
	TopK          *int              `json:"top_k,omitempty"`
	StopSequences []string          `json:"stop_sequences,omitempty"`
	Stream        bool              `json:"stream,omitempty"`
	Metadata      *struct {
		UserID string `json:"user_id,omitempty"`
	} `json:"metadata,omitempty"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicChoice struct {
	Type                   string `json:"type"` // auto, any, none, tool
	Name                   string `json:"name,omitempty"`
	DisableParallelToolUse bool   `json:"disable_parallel_tool_use,omitempty"`
}

// anthropicBlock is one content block inside a message or a tool_result.
type anthropicBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// image
	Source *anthropicSource `json:"source,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	// thinking
	Thinking string `json:"thinking,omitempty"`
}

type anthropicSource struct {
	Type      string `json:"type"` // base64 or url
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// anthropicResponse is the non-stream /v1/messages answer.
type anthropicResponse struct {
	ID           string           `json:"id"`
	Type         string           `json:"type"`
	Role         string           `json:"role"`
	Model        string           `json:"model"`
	Content      []map[string]any `json:"content"`
	StopReason   string           `json:"stop_reason"`
	StopSequence *string          `json:"stop_sequence"`
	Usage        anthropicUsage   `json:"usage"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// anthropicFinish maps OpenAI finish reasons to Anthropic stop reasons.
func anthropicFinish(reason string) string {
	switch reason {
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "content_filter":
		return "refusal"
	default:
		return "end_turn"
	}
}

// anthropicBlocks parses a polymorphic Anthropic content field (plain string
// or block array) into blocks.
func anthropicBlocks(raw json.RawMessage) ([]anthropicBlock, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "\"") {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("content string: %w", err)
		}
		if s == "" {
			return nil, nil
		}
		return []anthropicBlock{{Type: "text", Text: s}}, nil
	}
	var blocks []anthropicBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("content blocks: %w", err)
	}
	return blocks, nil
}

// toOpenAI converts the Anthropic request into the router's chat shape.
func (r *anthropicRequest) toOpenAI() (*models.ChatCompletionRequest, error) {
	out := &models.ChatCompletionRequest{
		Model:     models.ModelId(r.Model),
		Stream:    r.Stream,
		MaxTokens: r.MaxTokens,
	}
	if r.Temperature != nil {
		out.Temperature = *r.Temperature
	}
	if r.TopP != nil {
		out.TopP = *r.TopP
	}
	if len(r.StopSequences) > 0 {
		out.Stop = r.StopSequences
	}
	if r.Metadata != nil && r.Metadata.UserID != "" {
		uid := r.Metadata.UserID
		out.User = &uid
	}

	// System: plain string or text-block array becomes one system message.
	if len(r.System) > 0 && strings.TrimSpace(string(r.System)) != "null" {
		blocks, err := anthropicBlocks(r.System)
		if err != nil {
			return nil, fmt.Errorf("system: %w", err)
		}
		var texts []string
		for _, b := range blocks {
			if b.Type != "text" {
				return nil, fmt.Errorf("system: unsupported block type %q (text only)", b.Type)
			}
			if strings.TrimSpace(b.Text) != "" {
				texts = append(texts, b.Text)
			}
		}
		if len(texts) > 0 {
			out.Messages = append(out.Messages, models.ChatMessage{
				Role:    "system",
				Content: strings.Join(texts, "\n"),
			})
		}
	}

	for i, rawMsg := range r.Messages {
		var probe struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(rawMsg, &probe); err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", i, err)
		}
		switch probe.Role {
		case "user", "assistant":
		default:
			return nil, fmt.Errorf("messages[%d]: unsupported role %q (user and assistant only)", i, probe.Role)
		}
		blocks, err := anthropicBlocks(probe.Content)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", i, err)
		}
		if probe.Role == "assistant" {
			msg, err := anthropicAssistantMessage(i, blocks)
			if err != nil {
				return nil, err
			}
			out.Messages = append(out.Messages, msg)
			continue
		}
		// User turn: text/image blocks form one user message; every
		// tool_result block becomes a separate OpenAI tool message.
		var parts []models.ChatMessageContentPart
		var texts []string
		flushUser := func() {
			if len(parts) == 0 && len(texts) == 0 {
				return
			}
			msg := models.ChatMessage{Role: "user"}
			if len(parts) > 0 {
				msg.ContentParts = parts
			} else {
				msg.Content = strings.Join(texts, "\n")
			}
			out.Messages = append(out.Messages, msg)
			parts = nil
			texts = nil
		}
		for _, b := range blocks {
			switch b.Type {
			case "text":
				if strings.TrimSpace(b.Text) != "" {
					texts = append(texts, b.Text)
				}
			case "image":
				if b.Source == nil {
					return nil, fmt.Errorf("messages[%d]: image block without source", i)
				}
				var url string
				switch b.Source.Type {
				case "base64":
					if b.Source.MediaType == "" || b.Source.Data == "" {
						return nil, fmt.Errorf("messages[%d]: base64 image needs media_type and data", i)
					}
					url = "data:" + b.Source.MediaType + ";base64," + b.Source.Data
				case "url":
					if b.Source.URL == "" {
						return nil, fmt.Errorf("messages[%d]: url image needs url", i)
					}
					url = b.Source.URL
				default:
					return nil, fmt.Errorf("messages[%d]: unsupported image source type %q", i, b.Source.Type)
				}
				if len(texts) > 0 {
					parts = append(parts, models.ChatMessageContentPart{Type: "text", Text: strings.Join(texts, "\n")})
					texts = nil
				}
				parts = append(parts, models.ChatMessageContentPart{
					Type:     "image_url",
					ImageURL: &models.ChatMessageImageURL{URL: url},
				})
			case "tool_result":
				flushUser()
				content, err := anthropicToolResultText(i, b.Content)
				if err != nil {
					return nil, err
				}
				out.Messages = append(out.Messages, models.ChatMessage{
					Role:       "tool",
					ToolCallID: b.ToolUseID,
					Content:    content,
				})
			default:
				return nil, fmt.Errorf("messages[%d]: unsupported block type %q in user content", i, b.Type)
			}
		}
		flushUser()
	}

	if len(out.Messages) == 0 {
		return nil, fmt.Errorf("messages must not be empty")
	}

	for _, t := range r.Tools {
		if t.Name == "" {
			return nil, fmt.Errorf("tools: every tool needs a name")
		}
		out.Tools = append(out.Tools, models.ChatTool{
			Type: "function",
			Function: &models.ChatToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	if r.ToolChoice != nil {
		switch r.ToolChoice.Type {
		case "auto":
			out.ToolChoice = "auto"
		case "any":
			out.ToolChoice = "required"
		case "none":
			out.ToolChoice = "none"
		case "tool":
			if r.ToolChoice.Name == "" {
				return nil, fmt.Errorf("tool_choice type tool needs a name")
			}
			out.ToolChoice = map[string]any{
				"type":     "function",
				"function": map[string]any{"name": r.ToolChoice.Name},
			}
		case "":
			// Absent type behaves like auto.
			out.ToolChoice = "auto"
		default:
			return nil, fmt.Errorf("unsupported tool_choice type %q", r.ToolChoice.Type)
		}
		if r.ToolChoice.DisableParallelToolUse {
			f := false
			out.ParallelToolCalls = &f
		}
	}
	if r.Stream {
		out.StreamOptions = &models.StreamOptions{IncludeUsage: true}
	}
	return out, nil
}

func anthropicAssistantMessage(index int, blocks []anthropicBlock) (models.ChatMessage, error) {
	msg := models.ChatMessage{Role: "assistant"}
	var texts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) != "" {
				texts = append(texts, b.Text)
			}
		case "thinking", "redacted_thinking":
			if strings.TrimSpace(b.Thinking) != "" {
				if msg.ReasoningContent != "" {
					msg.ReasoningContent += "\n"
				}
				msg.ReasoningContent += b.Thinking
			}
		case "tool_use":
			args := "{}"
			if trimmed := strings.TrimSpace(string(b.Input)); trimmed != "" && trimmed != "null" {
				args = trimmed
			}
			msg.ToolCalls = append(msg.ToolCalls, models.ChatToolCall{
				ID:   b.ID,
				Type: "function",
				Function: models.ChatToolFunction{
					Name:      b.Name,
					Arguments: args,
				},
			})
		default:
			return msg, fmt.Errorf("messages[%d]: unsupported block type %q in assistant content", index, b.Type)
		}
	}
	msg.Content = strings.Join(texts, "\n")
	return msg, nil
}

func anthropicToolResultText(index int, raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", nil
	}
	blocks, err := anthropicBlocks(raw)
	if err != nil {
		return "", fmt.Errorf("messages[%d]: tool_result content: %w", index, err)
	}
	var texts []string
	for _, b := range blocks {
		if b.Type != "text" {
			return "", fmt.Errorf("messages[%d]: tool_result supports text blocks only", index)
		}
		if strings.TrimSpace(b.Text) != "" {
			texts = append(texts, b.Text)
		}
	}
	return strings.Join(texts, "\n"), nil
}

// anthropicFromOpenAI renders the chat completion as an Anthropic message.
func anthropicFromOpenAI(resp *models.ChatCompletionResponse, model string) *anthropicResponse {
	out := &anthropicResponse{
		ID:         resp.ID,
		Type:       "message",
		Role:       "assistant",
		Model:      model,
		Content:    []map[string]any{},
		StopReason: "end_turn",
		Usage: anthropicUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}
	if out.ID == "" {
		out.ID = "msg_router"
	}
	if len(resp.Choices) == 0 {
		return out
	}
	choice := resp.Choices[0]
	msg := choice.Message
	if strings.TrimSpace(msg.ReasoningContent) != "" {
		out.Content = append(out.Content, map[string]any{
			"type":     "thinking",
			"thinking": msg.ReasoningContent,
		})
	}
	if strings.TrimSpace(msg.Content) != "" {
		out.Content = append(out.Content, map[string]any{
			"type": "text",
			"text": msg.Content,
		})
	}
	for _, tc := range msg.ToolCalls {
		var input any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil || input == nil {
			input = map[string]any{}
		}
		out.Content = append(out.Content, map[string]any{
			"type":  "tool_use",
			"id":    tc.ID,
			"name":  tc.Function.Name,
			"input": input,
		})
	}
	out.StopReason = anthropicFinish(choice.FinishReason)
	return out
}
