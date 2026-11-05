package v1

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

var timeZero = time.Time{}

func TestStreamUsageTapKeepsLastUsageAcrossSplitFrames(t *testing.T) {
	var output bytes.Buffer
	tap := NewStreamMetricsTap(&output, nil)
	frames := `data: {"model":"mock/member","choices":[{"index":0,"delta":{"content":"a"}}]}` + "\n\n" +
		`data: {"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}` + "\n\n" +
		"data: [DONE]\n\n"
	// Split inside the usage frame: the tap must reassemble lines.
	cut := strings.Index(frames, `"completion_tokens"`)
	if _, err := tap.Write([]byte(frames[:cut])); err != nil {
		t.Fatal(err)
	}
	if _, err := tap.Write([]byte(frames[cut:])); err != nil {
		t.Fatal(err)
	}
	usage := tap.Usage()
	if usage == nil || usage.PromptTokens != 7 || usage.CompletionTokens != 3 {
		t.Fatalf("usage: %+v", usage)
	}
	if output.String() != frames {
		t.Fatal("tap must forward bytes unchanged")
	}
	if got := tap.Model("virtual/group"); got != "mock/member" {
		t.Fatalf("answering model = %q, want the stamped member", got)
	}
	if got := NewStreamMetricsTap(&output, nil).Model("virtual/group"); got != "virtual/group" {
		t.Fatalf("model without frames must fall back to the requested one, got %q", got)
	}
}

func TestBuildStreamMetricEventRecordsUsageAndClassifiedStatus(t *testing.T) {
	usage := &models.ChatCompletionUsage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7}
	event := BuildStreamMetricEvent(timeZero, "p/m", "p", "t", 0, nil, usage)
	if event.TokensInput != 5 || event.TokensOutput != 2 {
		t.Fatalf("tokens: %+v", event)
	}
	failed := BuildStreamMetricEvent(timeZero, "p/m", "p", "t", 0, &models.ProviderError{StatusCode: 429, Code: "rate_limit", Message: "slow down"}, nil)
	if failed.StatusCode != 429 || failed.ErrorType != "rate_limit" {
		t.Fatalf("failure classification: %+v", failed)
	}
}

func TestStreamErrorFrameUsesWireMessage(t *testing.T) {
	wrapped := errors.Join(errors.New("internal detail: sql: database is locked"), &models.ProviderError{StatusCode: 502, Code: "server_error", Message: "upstream failed"})
	payload, err := MarshalOpenAIStreamError(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "sql:") || !strings.Contains(string(payload), "upstream failed") {
		t.Fatalf("stream error must render the plugin message only: %s", payload)
	}
}
