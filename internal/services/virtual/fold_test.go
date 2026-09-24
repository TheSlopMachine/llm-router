package virtual

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func info(name string, caps, params, in, out []string, ctx, max int64, reasoning *models.ModelReasoning) models.ModelInfo {
	return models.ModelInfo{
		Name:                name,
		Capabilities:        caps,
		SupportedParameters: params,
		InputModalities:     in,
		OutputModalities:    out,
		ContextWindow:       ctx,
		MaxTokens:           max,
		Reasoning:           reasoning,
	}
}

func hasAll(hay []string, want ...string) bool {
	set := map[string]bool{}
	for _, v := range hay {
		set[v] = true
	}
	for _, v := range want {
		if !set[v] {
			return false
		}
	}
	return len(hay) == len(want)
}

func TestFoldMembers_IntersectionAndMinima(t *testing.T) {
	r := &models.ModelReasoning{SupportedEfforts: []string{"high", "low"}, DefaultEffort: "low", DefaultEnabled: true}
	members := []models.ModelInfo{
		info("a", []string{"tools", "json_mode", "vision"}, []string{"tools", "temperature"}, []string{"text", "image"}, []string{"text"}, 100000, 8000, r),
		info("b", []string{"tools", "json_mode"}, []string{"tools"}, []string{"text"}, []string{"text"}, 50000, 16000,
			&models.ModelReasoning{SupportedEfforts: []string{"low", "none"}, DefaultEffort: "low", DefaultEnabled: true}),
	}
	agg := FoldMembers(members)
	if !hasAll(agg.Capabilities, "tools", "json_mode") {
		t.Fatalf("caps: %v", agg.Capabilities)
	}
	if !hasAll(agg.SupportedParameters, "tools") {
		t.Fatalf("params: %v", agg.SupportedParameters)
	}
	if !hasAll(agg.InputModalities, "text") {
		t.Fatalf("in: %v", agg.InputModalities)
	}
	if !hasAll(agg.OutputModalities, "text") {
		t.Fatalf("out: %v", agg.OutputModalities)
	}
	if agg.ContextLength != 50000 || agg.MaxCompletionTokens != 8000 {
		t.Fatalf("limits: %d/%d", agg.ContextLength, agg.MaxCompletionTokens)
	}
	if agg.Reasoning == nil || !hasAll(agg.Reasoning.SupportedEfforts, "low") {
		t.Fatalf("reasoning: %+v", agg.Reasoning)
	}
}

func TestFoldMembers_NoReasoningWhenMemberLacks(t *testing.T) {
	r := &models.ModelReasoning{SupportedEfforts: []string{"low"}}
	members := []models.ModelInfo{
		info("a", nil, nil, []string{"text"}, []string{"text"}, 1000, 100, r),
		info("b", nil, nil, []string{"text"}, []string{"text"}, 1000, 100, nil),
	}
	if agg := FoldMembers(members); agg.Reasoning != nil {
		t.Fatalf("reasoning must be nil: %+v", agg.Reasoning)
	}
}

func TestFoldMembers_Empty(t *testing.T) {
	agg := FoldMembers(nil)
	if len(agg.Capabilities) != 0 || agg.ContextLength != 0 || agg.Reasoning != nil {
		t.Fatalf("zero metadata expected: %+v", agg)
	}
}
