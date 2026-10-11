package models

import (
	"encoding/json"
	"fmt"
	"strings"
)

// EstimateInputTokens approximates input tokens for count_tokens endpoints:
// 4 characters per token, minimum 1. Clients needing exact billing counts
// use upstream tokenizers; the router documents this heuristic in the API
// description.
func EstimateInputTokens(texts []string) int {
	total := 0
	for _, t := range texts {
		total += len(t)
	}
	if n := total / 4; n > 0 {
		return n
	}
	return 1
}

// AnthropicFinishReason maps OpenAI finish reasons to Anthropic stop reasons.
func AnthropicFinishReason(reason string) string {
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

// AnthropicBlocks parses a polymorphic Anthropic content field (plain string
// or block array) into blocks.
func AnthropicBlocks(raw json.RawMessage) ([]AnthropicContentBlock, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "\"") {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, fmt.Errorf("content string: %w", err)
		}
		if text == "" {
			return nil, nil
		}
		return []AnthropicContentBlock{{Type: "text", Text: text}}, nil
	}
	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("content blocks: %w", err)
	}
	return blocks, nil
}

// ToChat converts the Anthropic request into the router's chat shape.
// Document blocks are refused: the chat pipeline has no document part.
// Cache-control, top_k and top-level thinking budgets are accepted and
// documented; they carry no mapping.
func (r *AnthropicMessageRequest) ToChat() (*ChatCompletionRequest, error) {
	out := &ChatCompletionRequest{
		Model:     ModelId(r.Model),
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
		blocks, err := AnthropicBlocks(r.System)
		if err != nil {
			return nil, fmt.Errorf("system: %w", err)
		}
		var texts []string
		for _, block := range blocks {
			if block.Type != "text" {
				return nil, fmt.Errorf("system: unsupported block type %q (text only)", block.Type)
			}
			if strings.TrimSpace(block.Text) != "" {
				texts = append(texts, block.Text)
			}
		}
		if len(texts) > 0 {
			out.Messages = append(out.Messages, ChatMessage{
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
		blocks, err := AnthropicBlocks(probe.Content)
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
		var parts []ChatMessageContentPart
		var texts []string
		flushUser := func() {
			if len(parts) == 0 && len(texts) == 0 {
				return
			}
			msg := ChatMessage{Role: "user"}
			if len(parts) > 0 {
				msg.ContentParts = parts
			} else {
				msg.Content = strings.Join(texts, "\n")
			}
			out.Messages = append(out.Messages, msg)
			parts = nil
			texts = nil
		}
		for _, block := range blocks {
			switch block.Type {
			case "text":
				if strings.TrimSpace(block.Text) != "" {
					texts = append(texts, block.Text)
				}
			case "image":
				if block.Source == nil {
					return nil, fmt.Errorf("messages[%d]: image block without source", i)
				}
				var url string
				switch block.Source.Type {
				case "base64":
					if block.Source.MediaType == "" || block.Source.Data == "" {
						return nil, fmt.Errorf("messages[%d]: base64 image needs media_type and data", i)
					}
					url = "data:" + block.Source.MediaType + ";base64," + block.Source.Data
				case "url":
					if block.Source.URL == "" {
						return nil, fmt.Errorf("messages[%d]: url image needs url", i)
					}
					url = block.Source.URL
				default:
					return nil, fmt.Errorf("messages[%d]: unsupported image source type %q", i, block.Source.Type)
				}
				if len(texts) > 0 {
					parts = append(parts, ChatMessageContentPart{Type: "text", Text: strings.Join(texts, "\n")})
					texts = nil
				}
				parts = append(parts, ChatMessageContentPart{
					Type:     "image_url",
					ImageURL: &ChatMessageImageURL{URL: url},
				})
			case "tool_result":
				flushUser()
				content, err := anthropicToolResultText(i, block.Content)
				if err != nil {
					return nil, err
				}
				out.Messages = append(out.Messages, ChatMessage{
					Role:       "tool",
					ToolCallID: block.ToolUseID,
					Content:    content,
				})
			default:
				return nil, fmt.Errorf("messages[%d]: unsupported block type %q in user content", i, block.Type)
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
		out.Tools = append(out.Tools, ChatTool{
			Type: "function",
			Function: &ChatToolFunction{
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
		out.StreamOptions = &StreamOptions{IncludeUsage: true}
	}
	return out, nil
}

func anthropicAssistantMessage(index int, blocks []AnthropicContentBlock) (ChatMessage, error) {
	msg := ChatMessage{Role: "assistant"}
	var texts []string
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if strings.TrimSpace(block.Text) != "" {
				texts = append(texts, block.Text)
			}
		case "thinking", "redacted_thinking":
			if strings.TrimSpace(block.Thinking) != "" {
				if msg.ReasoningContent != "" {
					msg.ReasoningContent += "\n"
				}
				msg.ReasoningContent += block.Thinking
			}
		case "tool_use":
			args := "{}"
			if trimmed := strings.TrimSpace(string(block.Input)); trimmed != "" && trimmed != "null" {
				args = trimmed
			}
			msg.ToolCalls = append(msg.ToolCalls, ChatToolCall{
				ID:   block.ID,
				Type: "function",
				Function: ChatToolFunction{
					Name:      block.Name,
					Arguments: args,
				},
			})
		default:
			return msg, fmt.Errorf("messages[%d]: unsupported block type %q in assistant content", index, block.Type)
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
	blocks, err := AnthropicBlocks(raw)
	if err != nil {
		return "", fmt.Errorf("messages[%d]: tool_result content: %w", index, err)
	}
	var texts []string
	for _, block := range blocks {
		if block.Type != "text" {
			return "", fmt.Errorf("messages[%d]: tool_result supports text blocks only", index)
		}
		if strings.TrimSpace(block.Text) != "" {
			texts = append(texts, block.Text)
		}
	}
	return strings.Join(texts, "\n"), nil
}

// AnthropicMessageFromChat renders the chat completion as an Anthropic message.
func AnthropicMessageFromChat(resp *ChatCompletionResponse, model string) *AnthropicMessage {
	out := &AnthropicMessage{
		ID:         resp.ID,
		Type:       "message",
		Role:       "assistant",
		Model:      model,
		Content:    []map[string]any{},
		StopReason: "end_turn",
		Usage: AnthropicUsage{
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
	for _, toolCall := range msg.ToolCalls {
		var input any
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &input); err != nil || input == nil {
			input = map[string]any{}
		}
		out.Content = append(out.Content, map[string]any{
			"type":  "tool_use",
			"id":    toolCall.ID,
			"name":  toolCall.Function.Name,
			"input": input,
		})
	}
	out.StopReason = AnthropicFinishReason(choice.FinishReason)
	return out
}
