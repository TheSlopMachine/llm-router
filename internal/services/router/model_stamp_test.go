package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// Every chat response names the model that answered as its full router id,
// whatever the backend reported; a virtual request reports its member.
func TestRouterStampsAnsweringModel(t *testing.T) {
	routerService, _, _ := setupVirtualStack(t)

	direct, err := routerService.Complete(context.Background(), virtualRequest("mock/test-model"), nil)
	if err != nil {
		t.Fatalf("direct complete: %v", err)
	}
	if direct.Model != "mock/test-model" {
		t.Fatalf("direct model = %q, want mock/test-model", direct.Model)
	}

	virtual, err := routerService.Complete(context.Background(), virtualRequest("virtual/e2e"), nil)
	if err != nil {
		t.Fatalf("virtual complete: %v", err)
	}
	if virtual.Model != "mock/test-model" {
		t.Fatalf("virtual model = %q, want the answering member", virtual.Model)
	}
}

func TestRouterStampsAnsweringModelInStreams(t *testing.T) {
	routerService, _, _ := setupVirtualStack(t)
	for _, requested := range []string{"mock/test-model", "virtual/e2e"} {
		var output bytes.Buffer
		if err := routerService.CompleteStream(context.Background(), virtualRequest(requested), &output, nil); err != nil {
			t.Fatalf("%s stream: %v", requested, err)
		}
		frames := 0
		for _, line := range strings.Split(output.String(), "\n") {
			payload, ok := strings.CutPrefix(line, "data: ")
			if !ok || !strings.HasPrefix(payload, "{") {
				continue
			}
			var chunk struct {
				Model string `json:"model"`
			}
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				t.Fatalf("%s frame %q: %v", requested, payload, err)
			}
			if chunk.Model != "mock/test-model" {
				t.Fatalf("%s frame model = %q, want mock/test-model", requested, chunk.Model)
			}
			frames++
		}
		if frames == 0 {
			t.Fatalf("%s stream carried no data frames: %q", requested, output.String())
		}
	}
}

// The stamp keeps every other field and the frame layout, also when the
// backend splits a frame across writes or reports a bare upstream name.
func TestRouterStampPreservesFramesAcrossSplitWrites(t *testing.T) {
	adapter := testutil.NewMockAdapter("mock").WithCompleteStreamFunc(
		func(_ context.Context, _ []*models.Credential, _ *models.ChatCompletionRequest, writer io.Writer) error {
			frame := `data: {"id":"x","model":"gemini-2.5-flash","system_fingerprint":"fp","choices":[]}` + "\n\n"
			half := len(frame) / 2
			_, _ = io.WriteString(writer, frame[:half])
			_, _ = io.WriteString(writer, frame[half:])
			_, _ = io.WriteString(writer, "data: [DONE]\n\n")
			return nil
		})
	routerService, _, _ := setupVirtualStackWithAdapter(t, adapter)
	var output bytes.Buffer
	if err := routerService.CompleteStream(context.Background(), virtualRequest("mock/test-model"), &output, nil); err != nil {
		t.Fatalf("stream: %v", err)
	}
	text := output.String()
	if !strings.Contains(text, `"model":"mock/test-model"`) || strings.Contains(text, "gemini-2.5-flash") {
		t.Fatalf("model not stamped: %q", text)
	}
	if !strings.Contains(text, `"system_fingerprint":"fp"`) || !strings.Contains(text, "data: [DONE]\n") {
		t.Fatalf("other fields or terminator lost: %q", text)
	}
}
