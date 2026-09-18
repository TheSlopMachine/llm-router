package v1

import (
	"encoding/json"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestAnthropicToOpenAI_Basic(t *testing.T) {
	raw := `{
		"model": "google/gemini-2.5-flash",
		"max_tokens": 1024,
		"system": "You are terse.",
		"messages": [
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": [{"type":"text","text":"Hi there"}]},
			{"role": "user", "content": [{"type":"text","text":"How are you?"}]}
		],
		"temperature": 0.5,
		"stop_sequences": ["END"]
	}`
	var req anthropicRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	chat, err := req.toOpenAI()
	if err != nil {
		t.Fatal(err)
	}
	if chat.Model != "google/gemini-2.5-flash" || chat.MaxTokens != 1024 {
		t.Fatalf("scalars: %+v", chat)
	}
	if len(chat.Messages) != 4 {
		t.Fatalf("messages: %d", len(chat.Messages))
	}
	if chat.Messages[0].Role != "system" || chat.Messages[0].Content != "You are terse." {
		t.Fatalf("system: %+v", chat.Messages[0])
	}
	if chat.Messages[1].Role != "user" || chat.Messages[1].Content != "Hello" {
		t.Fatalf("user: %+v", chat.Messages[1])
	}
	if chat.Messages[2].Role != "assistant" || chat.Messages[2].Content != "Hi there" {
		t.Fatalf("assistant: %+v", chat.Messages[2])
	}
	if chat.Messages[3].Content != "How are you?" {
		t.Fatalf("blocks user: %+v", chat.Messages[3])
	}
	if chat.Temperature != 0.5 {
		t.Fatalf("temperature: %v", chat.Temperature)
	}
	stops, ok := chat.Stop.([]string)
	if !ok || len(stops) != 1 || stops[0] != "END" {
		t.Fatalf("stop: %+v", chat.Stop)
	}
}

func TestAnthropicToOpenAI_ToolRoundTrip(t *testing.T) {
	raw := `{
		"model": "google/gemini-2.5-flash",
		"max_tokens": 512,
		"messages": [
			{"role": "user", "content": "Weather in Paris?"},
			{"role": "assistant", "content": [
				{"type":"thinking","thinking":"need weather tool"},
				{"type":"text","text":"Let me check."},
				{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"Paris"}}
			]},
			{"role": "user", "content": [
				{"type":"tool_result","tool_use_id":"toolu_1","content":"Sunny, 22C"}
			]}
		],
		"tools": [{"name":"get_weather","description":"Get weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}],
		"tool_choice": {"type":"any"}
	}`
	var req anthropicRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	chat, err := req.toOpenAI()
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.Messages) != 3 {
		t.Fatalf("messages: %d", len(chat.Messages))
	}
	assistant := chat.Messages[1]
	if assistant.ReasoningContent != "need weather tool" {
		t.Fatalf("thinking: %q", assistant.ReasoningContent)
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "toolu_1" ||
		assistant.ToolCalls[0].Function.Name != "get_weather" ||
		assistant.ToolCalls[0].Function.Arguments != `{"city":"Paris"}` {
		t.Fatalf("tool calls: %+v", assistant.ToolCalls)
	}
	toolMsg := chat.Messages[2]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "toolu_1" || toolMsg.Content != "Sunny, 22C" {
		t.Fatalf("tool message: %+v", toolMsg)
	}
	if len(chat.Tools) != 1 || chat.Tools[0].Function == nil || chat.Tools[0].Function.Name != "get_weather" {
		t.Fatalf("tools: %+v", chat.Tools)
	}
	if chat.Tools[0].Function.Parameters["type"] != "object" {
		t.Fatalf("input_schema: %+v", chat.Tools[0].Function.Parameters)
	}
	if chat.ToolChoice != "required" {
		t.Fatalf("tool_choice: %+v", chat.ToolChoice)
	}
}

func TestAnthropicToOpenAI_ToolChoiceNamed(t *testing.T) {
	raw := `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":"x"}],
		"tool_choice":{"type":"tool","name":"get_weather","disable_parallel_tool_use":true}}`
	var req anthropicRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	chat, err := req.toOpenAI()
	if err != nil {
		t.Fatal(err)
	}
	choice, ok := chat.ToolChoice.(map[string]any)
	if !ok {
		t.Fatalf("tool_choice: %+v", chat.ToolChoice)
	}
	fn, _ := choice["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Fatalf("named choice: %+v", choice)
	}
	if chat.ParallelToolCalls == nil || *chat.ParallelToolCalls != false {
		t.Fatalf("parallel: %+v", chat.ParallelToolCalls)
	}
}

func TestAnthropicToOpenAI_ImageBase64(t *testing.T) {
	raw := `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[
		{"type":"text","text":"what is this?"},
		{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aVBORw=="}}
	]}]}`
	var req anthropicRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	chat, err := req.toOpenAI()
	if err != nil {
		t.Fatal(err)
	}
	parts := chat.Messages[0].ContentParts
	if len(parts) != 2 || parts[1].ImageURL == nil || parts[1].ImageURL.URL != "data:image/png;base64,aVBORw==" {
		t.Fatalf("parts: %+v", parts)
	}
}

func TestAnthropicToOpenAI_Rejects(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"system blocks non-text", `{"model":"m","max_tokens":1,"system":[{"type":"image"}],"messages":[{"role":"user","content":"x"}]}`},
		{"bad role", `{"model":"m","max_tokens":1,"messages":[{"role":"developer","content":"x"}]}`},
		{"document block", `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[{"type":"document"}]}]}`},
		{"bad tool choice", `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":"x"}],"tool_choice":{"type":"sometimes"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req anthropicRequest
			if err := json.Unmarshal([]byte(tc.raw), &req); err != nil {
				t.Fatal(err)
			}
			if _, err := req.toOpenAI(); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestAnthropicFromOpenAI_TextAndTools(t *testing.T) {
	resp := &models.ChatCompletionResponse{
		ID:    "chatcmpl-1",
		Model: "google/gemini-2.5-flash",
		Usage: models.ChatCompletionUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		Choices: []models.ChatCompletionChoice{{
			Index:        0,
			FinishReason: "tool_calls",
			Message: models.ChatMessage{
				Role:             "assistant",
				Content:          "checking",
				ReasoningContent: "thinking out loud",
				ToolCalls: []models.ChatToolCall{{
					ID:   "toolu_9",
					Type: "function",
					Function: models.ChatToolFunction{
						Name:      "get_weather",
						Arguments: `{"city":"Paris"}`,
					},
				}},
			},
		}},
	}
	out := anthropicFromOpenAI(resp, "google/gemini-2.5-flash")
	if out.Type != "message" || out.Role != "assistant" || out.Model != "google/gemini-2.5-flash" {
		t.Fatalf("envelope: %+v", out)
	}
	if out.StopReason != "tool_use" {
		t.Fatalf("stop reason: %q", out.StopReason)
	}
	if out.Usage.InputTokens != 10 || out.Usage.OutputTokens != 5 {
		t.Fatalf("usage: %+v", out.Usage)
	}
	if len(out.Content) != 3 {
		t.Fatalf("content blocks: %+v", out.Content)
	}
	if out.Content[0]["type"] != "thinking" || out.Content[0]["thinking"] != "thinking out loud" {
		t.Fatalf("thinking block: %+v", out.Content[0])
	}
	if out.Content[1]["type"] != "text" || out.Content[1]["text"] != "checking" {
		t.Fatalf("text block: %+v", out.Content[1])
	}
	if out.Content[2]["type"] != "tool_use" || out.Content[2]["name"] != "get_weather" {
		t.Fatalf("tool block: %+v", out.Content[2])
	}
	input, _ := out.Content[2]["input"].(map[string]any)
	if input["city"] != "Paris" {
		t.Fatalf("tool input: %+v", input)
	}
}

func TestAnthropicFinish(t *testing.T) {
	cases := map[string]string{
		"stop":           "end_turn",
		"length":         "max_tokens",
		"tool_calls":     "tool_use",
		"content_filter": "refusal",
		"":               "end_turn",
	}
	for in, want := range cases {
		if got := anthropicFinish(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}
