package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestChatMessage_ReasoningContent_RoundTrip(t *testing.T) {
	var m ChatMessage
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":"4.","reasoning_content":"2+2 is 4"}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.ReasoningContent != "2+2 is 4" {
		t.Fatalf("reasoning_content: got %q", m.ReasoningContent)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"reasoning_content":"2+2 is 4"`) {
		t.Fatalf("marshal lost reasoning_content: %s", raw)
	}
}

func TestChatMessage_ReasoningAlias(t *testing.T) {
	// Groq-style `reasoning` maps onto reasoning_content.
	var m ChatMessage
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":"4.","reasoning":"2+2 is 4"}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.ReasoningContent != "2+2 is 4" {
		t.Fatalf("reasoning alias: got %q", m.ReasoningContent)
	}
	raw, _ := json.Marshal(m)
	if strings.Contains(string(raw), `"reasoning"`) {
		t.Fatalf("alias must normalize to reasoning_content: %s", raw)
	}
}

func TestChatCompletionUsage_Details_RoundTrip(t *testing.T) {
	raw := `{"prompt_tokens":77,"completion_tokens":43,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":64},"completion_tokens_details":{"reasoning_tokens":29}}`
	var u ChatCompletionUsage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	if u.PromptTokensDetails == nil || u.PromptTokensDetails.CachedTokens != 64 {
		t.Fatalf("prompt_tokens_details: %+v", u.PromptTokensDetails)
	}
	if u.CompletionTokensDetails == nil || u.CompletionTokensDetails.ReasoningTokens != 29 {
		t.Fatalf("completion_tokens_details: %+v", u.CompletionTokensDetails)
	}
	out, _ := json.Marshal(u)
	if !strings.Contains(string(out), `"reasoning_tokens":29`) || !strings.Contains(string(out), `"cached_tokens":64`) {
		t.Fatalf("marshal lost details: %s", out)
	}
}

func TestChatMessage_MultimodalParts_RoundTrip(t *testing.T) {
	raw := `{"role":"user","content":[` +
		`{"type":"text","text":"what is this?"},` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,AAA","detail":"high"}},` +
		`{"type":"input_audio","input_audio":{"data":"BBB","format":"wav"}}]}`
	var m ChatMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.ContentParts) != 3 {
		t.Fatalf("parts: got %d", len(m.ContentParts))
	}
	if m.ContentParts[1].ImageURL == nil || m.ContentParts[1].ImageURL.URL != "data:image/png;base64,AAA" {
		t.Fatalf("image_url lost: %+v", m.ContentParts[1].ImageURL)
	}
	if m.ContentParts[2].InputAudio == nil || m.ContentParts[2].InputAudio.Format != "wav" {
		t.Fatalf("input_audio lost: %+v", m.ContentParts[2].InputAudio)
	}
	// Binary parts carry no text.
	if got := m.Content; got != "what is this?" {
		t.Fatalf("text flatten polluted: %q", got)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"image_url":{"url":"data:image/png;base64,AAA"`, `"input_audio":{"data":"BBB","format":"wav"}`} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("marshal lost multimodal part %s: %s", want, out)
		}
	}
}

func TestChatCompletionUsage_NoDetails_Omitted(t *testing.T) {
	out, _ := json.Marshal(ChatCompletionUsage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3})
	if strings.Contains(string(out), "_details") {
		t.Fatalf("absent details must be omitted: %s", out)
	}
}

func TestStreamChunk_UsageOnlyChunk(t *testing.T) {
	// The final chunk with stream_options.include_usage has empty choices
	// and only usage; it must round-trip.
	raw := `{"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":6,"total_tokens":11}}`
	var c StreamChunk
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	if c.Usage == nil || c.Usage.TotalTokens != 11 {
		t.Fatalf("usage: %+v", c.Usage)
	}
	if len(c.Choices) != 0 {
		t.Fatalf("choices: %+v", c.Choices)
	}
}
