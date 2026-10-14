package models

import (
	"encoding/json"
	"reflect"
	"testing"
)

// codexToolHistoryBody mirrors the input Codex 0.162 resends on a later turn: developer
// and user context, one shell call with its output, the assistant reply, and the next
// user turn. Codex resends the full history each turn and leaves previous_response_id unset.
const codexToolHistoryBody = `{
  "model": "harness-mock/codex-harness",
  "input": [
    {"type":"message","role":"developer","content":[{"type":"input_text","text":"You are a coding agent."}]},
    {"type":"message","role":"user","content":[{"type":"input_text","text":"PHASE 1 run=ci-1: create phase1.txt"}]},
    {"type":"function_call","id":"fc_call_1","call_id":"call_1","name":"exec_command","arguments":"{\"cmd\":\"echo phase-1 ok > phase1.txt\"}"},
    {"type":"function_call_output","call_id":"call_1","output":"Process exited with code 0\nOutput:\n"},
    {"type":"reasoning","summary":[]},
    {"type":"message","role":"assistant","content":[{"type":"output_text","text":"PHASE 1 COMPLETE","annotations":[]}]},
    {"type":"message","role":"user","content":[{"type":"input_text","text":"PHASE 2 run=ci-1: read phase1.txt"}]}
  ],
  "tools": [{"type":"function","name":"exec_command","description":"run","parameters":{"type":"object"}}],
  "stream": true
}`

func decodeResponseRequest(t *testing.T, raw string) ResponseRequest {
	t.Helper()
	var request ResponseRequest
	if err := json.Unmarshal([]byte(raw), &request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return request
}

func TestResponseRequestToChatKeepsToolHistory(t *testing.T) {
	request := decodeResponseRequest(t, codexToolHistoryBody)
	messages := request.ToChat().Messages

	gotRoles := make([]string, 0, len(messages))
	for _, message := range messages {
		gotRoles = append(gotRoles, message.Role)
	}
	wantRoles := []string{"system", "user", "assistant", "tool", "assistant", "user"}
	if !reflect.DeepEqual(gotRoles, wantRoles) {
		t.Fatalf("roles: got %v want %v", gotRoles, wantRoles)
	}

	calls := messages[2].ToolCalls
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Function.Name != "exec_command" ||
		calls[0].Function.Arguments != `{"cmd":"echo phase-1 ok > phase1.txt"}` {
		t.Fatalf("tool call: %+v", calls)
	}
	if messages[3].ToolCallID != "call_1" || messages[3].Content != "Process exited with code 0\nOutput:\n" {
		t.Fatalf("tool result: %+v", messages[3])
	}
	if messages[4].Content != "PHASE 1 COMPLETE" {
		t.Fatalf("assistant reply lost its text: %+v", messages[4])
	}
	if messages[5].Content != "PHASE 2 run=ci-1: read phase1.txt" {
		t.Fatalf("next user turn: %+v", messages[5])
	}
}

func TestResponseRequestToChatMapsDeveloperToSystem(t *testing.T) {
	request := decodeResponseRequest(t, `{"model":"m/x","input":[{"type":"message","role":"developer","content":"rules"}]}`)
	messages := request.ToChat().Messages
	if len(messages) != 1 || messages[0].Role != "system" || messages[0].Content != "rules" {
		t.Fatalf("developer turn: %+v", messages)
	}
}

func TestResponseRequestToChatFunctionOutputArrayText(t *testing.T) {
	request := decodeResponseRequest(t, `{"model":"m/x","input":[
		{"type":"function_call","call_id":"call_2","name":"exec_command","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_2","output":[{"type":"input_text","text":"first"},{"type":"input_text","text":"second"}]}
	]}`)
	messages := request.ToChat().Messages
	if len(messages) != 2 || messages[1].Role != "tool" || messages[1].Content != "first\nsecond" {
		t.Fatalf("array output: %+v", messages)
	}
}

func TestResponseObjectFromChatCarriesCallIdentifiers(t *testing.T) {
	request := decodeResponseRequest(t, `{"model":"m/x","input":"hi"}`)
	chat := &ChatCompletionResponse{Choices: []ChatCompletionChoice{{Message: ChatMessage{
		ToolCalls: []ChatToolCall{{ID: "call_9", Type: "function", Function: ChatToolFunction{Name: "exec_command", Arguments: `{"cmd":"ls"}`}}},
	}}}}
	response := ResponseObjectFromChat("resp_1", &request, chat)
	if len(response.Output) != 1 {
		t.Fatalf("output: %+v", response.Output)
	}
	item := response.Output[0]
	if item.Type != "function_call" || item.CallID != "call_9" || item.Arguments != `{"cmd":"ls"}` || item.Name != "exec_command" {
		t.Fatalf("function_call item: %+v", item)
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("encode item: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	if fields["call_id"] != "call_9" || fields["arguments"] != `{"cmd":"ls"}` {
		t.Fatalf("wire fields: %s", encoded)
	}
}

func TestResponseObjectFromChatMessageHasOutputTextContent(t *testing.T) {
	request := decodeResponseRequest(t, `{"model":"m/x","input":"hi"}`)
	chat := &ChatCompletionResponse{Choices: []ChatCompletionChoice{{Message: ChatMessage{Content: "hello"}}}}
	response := ResponseObjectFromChat("resp_2", &request, chat)
	if len(response.Output) != 1 {
		t.Fatalf("output: %+v", response.Output)
	}
	item := response.Output[0]
	if item.Role != "assistant" || item.Text != "hello" || len(item.Content) != 1 ||
		item.Content[0].Type != "output_text" || item.Content[0].Text != "hello" {
		t.Fatalf("message item: %+v", item)
	}
}

func TestResponseStreamEventsSequence(t *testing.T) {
	request := decodeResponseRequest(t, `{"model":"m/x","input":"hi"}`)
	chat := &ChatCompletionResponse{Choices: []ChatCompletionChoice{{Message: ChatMessage{
		Content:   "working",
		ToolCalls: []ChatToolCall{{ID: "call_5", Type: "function", Function: ChatToolFunction{Name: "exec_command", Arguments: `{"cmd":"pwd"}`}}},
	}}}}
	response := ResponseObjectFromChat("resp_3", &request, chat)

	events := ResponseStreamEvents(response)
	var names []string
	for _, event := range events {
		names = append(names, event.Name)
		if event.Data["type"] != event.Name {
			t.Fatalf("payload type %v does not match event %s", event.Data["type"], event.Name)
		}
		if _, err := json.Marshal(event.Data); err != nil {
			t.Fatalf("payload of %s does not encode: %v", event.Name, err)
		}
	}
	wantNames := []string{
		"response.created",
		"response.output_item.added",
		"response.content_part.added",
		"response.output_text.delta",
		"response.output_text.done",
		"response.content_part.done",
		"response.output_item.done",
		"response.output_item.added",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.output_item.done",
		"response.completed",
	}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("event order:\n got %v\nwant %v", names, wantNames)
	}

	created := events[0].Data["response"].(ResponseObject)
	if created.Status != "in_progress" || len(created.Output) != 0 {
		t.Fatalf("created snapshot: %+v", created)
	}
	argumentsDelta := events[8]
	if argumentsDelta.Data["delta"] != `{"cmd":"pwd"}` {
		t.Fatalf("arguments delta: %+v", argumentsDelta.Data)
	}
	finalCall := events[10].Data["item"].(ResponseOutputItem)
	if finalCall.Type != "function_call" || finalCall.CallID != "call_5" || finalCall.Arguments != `{"cmd":"pwd"}` {
		t.Fatalf("closed function call: %+v", finalCall)
	}
}
