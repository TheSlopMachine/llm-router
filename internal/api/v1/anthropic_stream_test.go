package v1

import (
	"strings"
	"testing"
)

// anthropicEvents splits an Anthropic SSE transcript into "event: X" names
// in arrival order.
func anthropicEvents(transcript string) []string {
	var output []string
	for _, line := range strings.Split(transcript, "\n") {
		if strings.HasPrefix(line, "event: ") {
			output = append(output, strings.TrimPrefix(line, "event: "))
		}
	}
	return output
}

func TestAnthropicStreamWriter_TextThenTool(t *testing.T) {
	var builder strings.Builder
	translatedWriter := newAnthropicStreamWriter(&builder, nil, "google/gemini-2.5-flash")

	chunks := []string{
		`{"id":"chatcmpl-9","object":"chat.completion.chunk","created":1,"model":"google/gemini-2.5-flash","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"thinking "}}]}`,
		`{"id":"chatcmpl-9","object":"chat.completion.chunk","created":1,"model":"google/gemini-2.5-flash","choices":[{"index":0,"delta":{"reasoning_content":"hard"}}]}`,
		`{"id":"chatcmpl-9","object":"chat.completion.chunk","created":1,"model":"google/gemini-2.5-flash","choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
		`{"id":"chatcmpl-9","object":"chat.completion.chunk","created":1,"model":"google/gemini-2.5-flash","choices":[{"index":0,"delta":{"content":" world"}}]}`,
		`{"id":"chatcmpl-9","object":"chat.completion.chunk","created":1,"model":"google/gemini-2.5-flash","choices":[{"index":0,"delta":{"tool_calls":[{"id":"toolu_1","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
		`{"id":"chatcmpl-9","object":"chat.completion.chunk","created":1,"model":"google/gemini-2.5-flash","choices":[{"index":0,"delta":{"tool_calls":[{"function":{"arguments":"{\"city\":"}}]}}]}`,
		`{"id":"chatcmpl-9","object":"chat.completion.chunk","created":1,"model":"google/gemini-2.5-flash","choices":[{"index":0,"delta":{"tool_calls":[{"function":{"arguments":"\"Paris\"}"}}]}}]}`,
		`{"id":"chatcmpl-9","object":"chat.completion.chunk","created":1,"model":"google/gemini-2.5-flash","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"id":"chatcmpl-9","object":"chat.completion.chunk","created":1,"model":"google/gemini-2.5-flash","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`,
	}
	for _, c := range chunks {
		if _, err := translatedWriter.Write([]byte("data: " + c + "\n\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if _, err := translatedWriter.Write([]byte("data: [DONE]\n\n")); err != nil {
		t.Fatalf("done: %v", err)
	}

	got := anthropicEvents(builder.String())
	want := []string{
		"message_start",
		"content_block_start", "content_block_delta", "content_block_delta", // thinking
		"content_block_stop",
		"content_block_start", "content_block_delta", "content_block_delta", // text
		"content_block_stop",
		"content_block_start", "content_block_delta", "content_block_delta", // tool_use
		"content_block_stop",
		"message_delta", "message_stop",
	}
	if len(got) != len(want) {
		t.Fatalf("events:\ngot  %v\nwant %v\ntranscript:\n%s", got, want, builder.String())
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d: got %q want %q\ntranscript:\n%s", i, got[i], want[i], builder.String())
		}
	}

	body := builder.String()
	for _, needle := range []string{
		`"type":"thinking_delta"`, `"thinking":"thinking "`, `"thinking":"hard"`,
		`"type":"text_delta"`, `"text":"Hello"`, `"text":" world"`,
		`"type":"tool_use"`, `"id":"toolu_1"`, `"name":"get_weather"`,
		`"type":"input_json_delta"`, `"partial_json":"{\"city\":"`, `\"Paris\"}`,
		`"stop_reason":"tool_use"`, `"output_tokens":7`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("transcript missing %s\n%s", needle, body)
		}
	}
}

func TestAnthropicStreamWriter_FragmentedFrames(t *testing.T) {
	var builder strings.Builder
	translatedWriter := newAnthropicStreamWriter(&builder, nil, "m")
	chunk := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}` + "\n\n" + `data: [DONE]` + "\n\n"
	// Feed byte by byte: frames must reassemble.
	for i := 0; i < len(chunk); i++ {
		if _, err := translatedWriter.Write([]byte(chunk[i : i+1])); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	got := anthropicEvents(builder.String())
	want := []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	if len(got) != len(want) {
		t.Fatalf("events: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d: got %q want %q", i, got[i], want[i])
		}
	}
}
