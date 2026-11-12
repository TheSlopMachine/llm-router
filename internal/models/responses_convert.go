package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// responseInputMessages converts a Responses input into chat turns. A string
// input is one user turn. Item arrays map per item type: messages keep their
// role (input_image and input_file blocks become content parts), function
// calls become assistant tool calls, function call outputs become tool turns,
// and reasoning or built-in tool items carry no chat turn.
func responseInputMessages(raw json.RawMessage) ([]ChatMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if strings.TrimSpace(single) == "" {
			return nil, nil
		}
		return []ChatMessage{{Role: "user", Content: single}}, nil
	}
	var items []ResponseInputItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, nil
	}
	var messages []ChatMessage
	for _, item := range items {
		switch item.Type {
		case "", "message":
			text, parts, err := responseMessageContent(item.Content)
			if err != nil {
				return nil, err
			}
			if text == "" && len(parts) == 0 {
				continue
			}
			messages = append(messages, ChatMessage{Role: messageRole(item.Role), Content: text, ContentParts: parts})
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
	return messages, nil
}

// responseMessageContent maps a message item's content onto chat content.
// A string or text-only blocks stay plain text. input_image and input_file
// blocks turn the turn into content parts that keep the block order. Files
// travel inline only: file_id needs a file store the router does not keep,
// and remote file_url sources are forbidden.
func responseMessageContent(raw json.RawMessage) (string, []ChatMessageContentPart, error) {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return single, nil, nil
	}
	var blocks []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL string `json:"image_url"`
		Detail   string `json:"detail"`
		FileID   string `json:"file_id"`
		FileData string `json:"file_data"`
		FileURL  string `json:"file_url"`
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", nil, nil
	}
	texts := make([]string, 0, len(blocks))
	parts := make([]ChatMessageContentPart, 0, len(blocks))
	attachments := false
	for _, block := range blocks {
		switch block.Type {
		case "input_image":
			if block.ImageURL == "" {
				return "", nil, fmt.Errorf("%w: input_image needs image_url; file_id is not supported, the router keeps no file store", ErrInvalidAttachment)
			}
			parts = append(parts, ChatMessageContentPart{Type: "image_url", ImageURL: &ChatMessageImageURL{URL: block.ImageURL, Detail: block.Detail}})
			attachments = true
		case "input_file":
			if block.FileURL != "" {
				return "", nil, fmt.Errorf("%w: input_file url sources are forbidden", ErrInvalidAttachment)
			}
			file, err := newChatMessageFile(block.FileData, block.FileID, block.Filename)
			if err != nil {
				return "", nil, err
			}
			parts = append(parts, ChatMessageContentPart{Type: "file", File: file})
			attachments = true
		default:
			if strings.TrimSpace(block.Text) != "" {
				texts = append(texts, block.Text)
				parts = append(parts, ChatMessageContentPart{Type: "text", Text: block.Text})
			}
		}
	}
	text := strings.Join(texts, "\n")
	if !attachments {
		return text, nil, nil
	}
	return text, parts, nil
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

// answeringModel names the model that produced response: the router stamps
// it (a virtual request carries its member). The requested model fills in
// only when the response carries none.
func answeringModel(requested ModelId, response *ChatCompletionResponse) string {
	if response != nil && response.Model != "" {
		return response.Model
	}
	return requested.String()
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
		Model:              answeringModel(request.Model, response),
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
