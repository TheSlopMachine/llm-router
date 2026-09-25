package generic

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestPassthroughPayloadForwardsAllFields(t *testing.T) {
	seed := int64(42)
	user := "u-1"
	tier := "flex"
	effort := "high"
	req := &models.ChatCompletionRequest{
		Model:           "custom/gpt-5",
		Messages:        []models.ChatMessage{{Role: "user", Content: "hi"}},
		Temperature:     0,
		Seed:            &seed,
		User:            &user,
		ServiceTier:     &tier,
		ReasoningEffort: &effort,
		ResponseFormat:  map[string]any{"type": "json_object"},
		ToolChoice:      "auto",
	}
	payload, err := passthroughPayload(req, "gpt-5-upstream")
	if err != nil {
		t.Fatalf("passthrough: %v", err)
	}
	if payload["model"] != "gpt-5-upstream" {
		t.Errorf("model not replaced: %v", payload["model"])
	}
	for _, key := range []string{"messages", "seed", "user", "service_tier", "reasoning_effort", "response_format", "tool_choice"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("field %q dropped from payload", key)
		}
	}
	if _, ok := payload["temperature"]; ok {
		t.Errorf("zero temperature should stay omitted, got %v", payload["temperature"])
	}
}
