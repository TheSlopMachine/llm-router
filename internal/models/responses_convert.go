package models

import (
	"encoding/json"
	"strings"
	"time"
)

// responseInputMessages converts a Responses input into chat turns. A string
// input is one user turn. Item arrays map per item type: messages keep their
// role, function calls become assistant tool calls, function call outputs
// become tool turns, and reasoning or built-in tool items carry no chat turn.
func responseInputMessages(raw json.RawMessage) []ChatMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if strings.TrimSpace(single) == "" {
			return nil
		}
		return []ChatMessage{{Role: "user", Content: single}}
	}
	var items []ResponseInputItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	var messages []ChatMessage
	for _, item := range items {
		switch item.Type {
		case "", "message":
			text := contentText(item.Content)
			if text == "" {
				continue
			}
			messages = append(messages, ChatMessage{Role: messageRole(item.Role), Content: text})
		case "function_call":
			callID := item.CallID
			if callID == "" {
				callID = item.ID
			}
			messages = append(messages, ChatMessage{
				Role: "assistant",
				ToolCalls: []ChatToolCall{{
					ID:       callID,
					Type:     "function",
					Function: ChatToolFunction{Name: item.Name, Arguments: item.Arguments},
				}},
			})
		case "function_call_output":
			messages = append(messages, ChatMessage{
				Role:       "tool",
				ToolCallID: item.CallID,
				Content:    contentText(item.Output),
			})
		}
	}
	return messages
}

// messageRole maps a Responses message role onto a chat role. Developer
// instructions become system turns, the chat role chat models accept.
func messageRole(role string) string {
	switch role {
	case "assistant":
		return "assistant"
	case "system", "developer":
		return "system"
	default:
		return "user"
	}
}

// contentText reads text from a content field that is either a string or an
// array of typed blocks. Blocks without text contribute nothing.
func contentText(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if strings.TrimSpace(block.Text) != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// ResponseObjectFromChat maps one chat completion onto a completed response
// object. Assistant text becomes a message item. Each tool call becomes a
// function_call item whose call_id is the identifier the client echoes back in
// its function_call_output item.
func ResponseObjectFromChat(id string, request *ResponseRequest, response *ChatCompletionResponse) *ResponseObject {
	output := &ResponseObject{
		ID:                 id,
		Object:             "response",
		CreatedAt:          time.Now().Unix(),
		Model:              request.Model.String(),
		Status:             "completed",
		Output:             []ResponseOutputItem{},
		Instructions:       request.Instructions,
		PreviousResponseID: request.PreviousResponseID,
		ConversationID:     request.ConversationID,
		Metadata:           request.Metadata,
	}
	if len(response.Choices) == 0 {
		return output
	}
	message := response.Choices[0].Message
	if message.Content != "" {
		output.Output = append(output.Output, messageItem(id, message.Content))
	}
	for _, toolCall := range message.ToolCalls {
		output.Output = append(output.Output, ResponseOutputItem{
			Type:      "function_call",
			ID:        "fc_" + toolCall.ID,
			Status:    "completed",
			Name:      toolCall.Function.Name,
			CallID:    toolCall.ID,
			Arguments: toolCall.Function.Arguments,
		})
	}
	output.Usage = &ResponseUsage{
		InputTokens:  response.Usage.PromptTokens,
		OutputTokens: response.Usage.CompletionTokens,
		TotalTokens:  response.Usage.TotalTokens,
	}
	return output
}

// messageItem builds a completed assistant message item for one response.
func messageItem(responseID, text string) ResponseOutputItem {
	return ResponseOutputItem{
		Type:    "message",
		ID:      "msg_" + responseID,
		Status:  "completed",
		Role:    "assistant",
		Text:    text,
		Content: []ResponseContentPart{{Type: "output_text", Text: text, Annotations: []any{}}},
	}
}

// ResponseStreamEvent is one server-sent event of a Responses stream: the SSE
// event name and the JSON payload. The payload's type field repeats the name.
type ResponseStreamEvent struct {
	Name string
	Data map[string]any
}

// ResponseStreamEvents expands a completed response into the event sequence a
// Responses client consumes: response.created, the added, delta and done events
// of each output item in order, then response.completed. Every payload carries
// its type field, and the item indexes the client matches on.
func ResponseStreamEvents(response *ResponseObject) []ResponseStreamEvent {
	creating := *response
	creating.Status = "in_progress"
	creating.Output = []ResponseOutputItem{}
	events := []ResponseStreamEvent{
		streamEvent("response.created", map[string]any{"response": creating}),
	}
	for index, item := range response.Output {
		switch item.Type {
		case "message":
			events = append(events, messageStreamEvents(index, item)...)
		case "function_call":
			events = append(events, functionCallStreamEvents(index, item)...)
		}
	}
	return append(events, streamEvent("response.completed", map[string]any{"response": response}))
}

// streamEvent builds one event whose payload carries its own type field.
func streamEvent(name string, fields map[string]any) ResponseStreamEvent {
	fields["type"] = name
	return ResponseStreamEvent{Name: name, Data: fields}
}

// messageStreamEvents streams one assistant message: the item opens, its
// output_text part opens, the text arrives in one delta, then the part and the
// item close.
func messageStreamEvents(index int, item ResponseOutputItem) []ResponseStreamEvent {
	text := ""
	if len(item.Content) > 0 {
		text = item.Content[0].Text
	}
	open := item
	open.Status = "in_progress"
	open.Text = ""
	open.Content = []ResponseContentPart{}
	emptyPart := ResponseContentPart{Type: "output_text", Text: "", Annotations: []any{}}
	donePart := ResponseContentPart{Type: "output_text", Text: text, Annotations: []any{}}
	return []ResponseStreamEvent{
		streamEvent("response.output_item.added", map[string]any{"output_index": index, "item": open}),
		streamEvent("response.content_part.added", map[string]any{"item_id": item.ID, "output_index": index, "content_index": 0, "part": emptyPart}),
		streamEvent("response.output_text.delta", map[string]any{"item_id": item.ID, "output_index": index, "content_index": 0, "delta": text}),
		streamEvent("response.output_text.done", map[string]any{"item_id": item.ID, "output_index": index, "content_index": 0, "text": text}),
		streamEvent("response.content_part.done", map[string]any{"item_id": item.ID, "output_index": index, "content_index": 0, "part": donePart}),
		streamEvent("response.output_item.done", map[string]any{"output_index": index, "item": item}),
	}
}

// functionCallStreamEvents streams one function call: the item opens with empty
// arguments, the arguments arrive in one delta, then the arguments and the item
// close.
func functionCallStreamEvents(index int, item ResponseOutputItem) []ResponseStreamEvent {
	open := item
	open.Status = "in_progress"
	open.Arguments = ""
	return []ResponseStreamEvent{
		streamEvent("response.output_item.added", map[string]any{"output_index": index, "item": open}),
		streamEvent("response.function_call_arguments.delta", map[string]any{"item_id": item.ID, "output_index": index, "delta": item.Arguments}),
		streamEvent("response.function_call_arguments.done", map[string]any{"item_id": item.ID, "output_index": index, "arguments": item.Arguments}),
		streamEvent("response.output_item.done", map[string]any{"output_index": index, "item": item}),
	}
}
