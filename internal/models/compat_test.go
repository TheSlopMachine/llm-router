package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompletionRequest_PromptShapes(t *testing.T) {
	var single CompletionRequest
	if err := json.Unmarshal([]byte(`{"model":"mock/m","prompt":"hi"}`), &single); err != nil {
		t.Fatalf("string prompt: %v", err)
	}
	if len(single.Prompt) != 1 || single.Prompt[0] != "hi" {
		t.Fatalf("prompt: %+v", single.Prompt)
	}
	var list CompletionRequest
	if err := json.Unmarshal([]byte(`{"model":"mock/m","prompt":["a","b"]}`), &list); err != nil {
		t.Fatalf("list prompt: %v", err)
	}
	if len(list.Prompt) != 2 {
		t.Fatalf("prompt: %+v", list.Prompt)
	}
	var tokens CompletionRequest
	if err := json.Unmarshal([]byte(`{"model":"mock/m","prompt":[1,2,3]}`), &tokens); err == nil {
		t.Fatal("token-id prompt must be refused")
	}
	var missing CompletionRequest
	if err := json.Unmarshal([]byte(`{"model":"mock/m"}`), &missing); err == nil {
		t.Fatal("missing prompt must be refused")
	}
}

func TestCompletionRequest_ToChat(t *testing.T) {
	seed := int64(7)
	req := CompletionRequest{
		Model:   "mock/m",
		Prompt:  []string{"hello"},
		Suffix:  " world",
		System:  "sys",
		Seed:    &seed,
		Images:  []string{"https://x/y.png"},
		Options: map[string]any{"temperature": 0.5, "num_predict": float64(32)},
	}
	req.NormalizeAliases()
	chat := req.ToChat()
	if len(chat.Messages) != 2 {
		t.Fatalf("messages: %d", len(chat.Messages))
	}
	if chat.Messages[0].Role != "system" || chat.Messages[0].Content != "sys" {
		t.Fatalf("system: %+v", chat.Messages[0])
	}
	user := chat.Messages[1]
	if !strings.Contains(user.Content, "hello world") {
		t.Fatalf("prompt+suffix: %q", user.Content)
	}
	if len(user.ContentParts) != 2 {
		t.Fatalf("parts: %+v", user.ContentParts)
	}
	if chat.MaxTokens != 32 || chat.Temperature != 0.5 {
		t.Fatalf("options: %+v", chat)
	}
}

func TestModerationRequest_Shapes(t *testing.T) {
	var single ModerationRequest
	if err := json.Unmarshal([]byte(`{"model":"mock/m","input":"bad"}`), &single); err != nil {
		t.Fatalf("string: %v", err)
	}
	var list ModerationRequest
	if err := json.Unmarshal([]byte(`{"model":"mock/m","input":["a","b"]}`), &list); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Input) != 2 {
		t.Fatalf("input: %+v", list.Input)
	}
	var parts ModerationRequest
	if err := json.Unmarshal([]byte(`{"model":"mock/m","input":[{"text":"x"},{"content":"y"}]}`), &parts); err != nil {
		t.Fatalf("parts: %v", err)
	}
	if len(parts.Input) != 2 || parts.Input[1] != "y" {
		t.Fatalf("input: %+v", parts.Input)
	}
	var missing ModerationRequest
	if err := json.Unmarshal([]byte(`{"model":"mock/m"}`), &missing); err == nil {
		t.Fatal("missing input must be refused")
	}
}

func TestChatCompletionRequest_NormalizeAliases(t *testing.T) {
	var req ChatCompletionRequest
	raw := `{"model":"mock/m","messages":[],"random_seed":42,"reasoning":{"effort":"high","max_tokens":500},"format":{"type":"json_object"},"options":{"temperature":0.3,"top_p":0.9,"seed":11,"stop":["x"],"num_predict":64}}`
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	req.NormalizeAliases()
	if req.Seed == nil || *req.Seed != 42 {
		t.Fatalf("random_seed: %+v", req.Seed)
	}
	if req.ReasoningEffort == nil || *req.ReasoningEffort != "high" {
		t.Fatalf("reasoning effort: %+v", req.ReasoningEffort)
	}
	if req.MaxCompletionTokens == nil || *req.MaxCompletionTokens != 500 {
		t.Fatalf("reasoning max tokens: %+v", req.MaxCompletionTokens)
	}
	if req.ResponseFormat == nil {
		t.Fatal("format must map to response_format")
	}
	if req.Temperature != 0.3 || req.TopP != 0.9 {
		t.Fatalf("options: %+v", req)
	}
	if req.MaxTokens != 64 {
		t.Fatalf("num_predict: %d", req.MaxTokens)
	}
}

func TestEmbeddingsRequest_NormalizeAliases(t *testing.T) {
	var req EmbeddingsRequest
	if err := json.Unmarshal([]byte(`{"model":"mock/m","input":"hi","output_dimension":1536,"output_dtype":"float32"}`), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Dimensions != 1536 {
		t.Fatalf("output_dimension: %d", req.Dimensions)
	}
	if len(req.Input) != 1 {
		t.Fatalf("input: %+v", req.Input)
	}
}

func TestResponseRequest_ToChat(t *testing.T) {
	raw := `{"model":"mock/m","instructions":"be nice","input":[{"type":"message","role":"user","content":"hi"}],"tools":[{"type":"function","name":"f","description":"d"}]}`
	var req ResponseRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	chat := req.ToChat()
	if len(chat.Messages) != 2 {
		t.Fatalf("messages: %+v", chat.Messages)
	}
	if chat.Messages[0].Role != "system" || len(chat.Tools) != 1 {
		t.Fatalf("chat: %+v", chat)
	}
	var strReq ResponseRequest
	if err := json.Unmarshal([]byte(`{"model":"mock/m","input":"hello"}`), &strReq); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if texts := strReq.InputTexts(); len(texts) != 1 || texts[0] != "hello" {
		t.Fatalf("texts: %+v", texts)
	}
}

func TestThreadMessageRequest_Texts(t *testing.T) {
	var req ThreadMessageRequest
	if err := json.Unmarshal([]byte(`{"role":"user","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}`), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if texts := req.ThreadMessageTexts(); len(texts) != 2 {
		t.Fatalf("texts: %+v", texts)
	}
}

func TestAnthropicCompleteRequest_ToMessageRequest(t *testing.T) {
	req := AnthropicCompleteRequest{Model: "mock/m", Prompt: "hi", MaxTokensToSample: 16}
	msg := req.ToMessageRequest()
	if msg.Model != "mock/m" || msg.MaxTokens != 16 || len(msg.Messages) != 1 {
		t.Fatalf("message request: %+v", msg)
	}
	chat, err := msg.ToChat()
	if err != nil {
		t.Fatalf("to chat: %v", err)
	}
	if len(chat.Messages) != 1 || chat.Messages[0].Content != "hi" {
		t.Fatalf("chat: %+v", chat.Messages)
	}
}

func TestBatchRecord_InfoAndResults(t *testing.T) {
	rec := &BatchRecord{ID: "batch_x", Entries: []BatchEntry{{CustomID: "a"}, {CustomID: "b"}}}
	rec.Results = []BatchResultLine{
		{CustomID: "a", Result: json.RawMessage(`{"type":"succeeded","message":{}}`)},
		{CustomID: "b", Result: json.RawMessage(`{"type":"errored","error":{}}`)},
	}
	info := rec.ToBatchInfo("/results")
	if info.RequestCounts.Succeeded != 1 || info.RequestCounts.Errored != 1 || info.RequestCounts.Processing != 0 {
		t.Fatalf("counts: %+v", info.RequestCounts)
	}
	lines := rec.BatchResults()
	if strings.Count(lines, "\n") != 2 || !strings.Contains(lines, `"custom_id":"a"`) {
		t.Fatalf("jsonl: %q", lines)
	}
}

func TestEstimateInputTokens(t *testing.T) {
	if got := EstimateInputTokens([]string{"abcd"}); got != 1 {
		t.Fatalf("got %d", got)
	}
	if got := EstimateInputTokens(nil); got != 1 {
		t.Fatalf("empty got %d", got)
	}
	if got := EstimateInputTokens([]string{strings.Repeat("a", 400)}); got != 100 {
		t.Fatalf("got %d", got)
	}
}

func TestIDPrefixes(t *testing.T) {
	for id, prefix := range map[string]string{
		NewResponseID(): "resp_", NewAssistantID(): "asst_", NewThreadID(): "thread_",
		NewThreadMessageID(): "msg_", NewRunID(): "run_",
		NewBatchID(): "batch_", NewConversationID(): "conv_",
		NewModerationID(): "modr_",
	} {
		if !strings.HasPrefix(id, prefix) {
			t.Fatalf("id %q missing prefix %q", id, prefix)
		}
	}
}
