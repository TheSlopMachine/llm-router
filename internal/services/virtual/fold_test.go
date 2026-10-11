package virtual

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func modelWithCapabilities(name string, capabilities, parameters, inputs, outputs []string, contextLength, maxTokens int64, reasoning *models.ModelReasoning) models.ModelInfo {
	return models.ModelInfo{
		Name:                name,
		Capabilities:        capabilities,
		SupportedParameters: parameters,
		InputModalities:     inputs,
		OutputModalities:    outputs,
		ContextWindow:       contextLength,
		MaxTokens:           maxTokens,
		Reasoning:           reasoning,
	}
}

func hasAll(haystack []string, want ...string) bool {
	set := map[string]bool{}
	for _, v := range haystack {
		set[v] = true
	}
	for _, v := range want {
		if !set[v] {
			return false
		}
	}
	return len(haystack) == len(want)
}

func TestFoldMembers_IntersectionAndMinima(t *testing.T) {
	reasoning := &models.ModelReasoning{SupportedEfforts: []string{"high", "low"}, DefaultEffort: "low", DefaultEnabled: true}
	members := []models.ModelInfo{
		modelWithCapabilities("a", []string{"tools", "json_mode", "vision"}, []string{"tools", "temperature"}, []string{"text", "image"}, []string{"text"}, 100000, 8000, reasoning),
		modelWithCapabilities("b", []string{"tools", "json_mode"}, []string{"tools"}, []string{"text"}, []string{"text"}, 50000, 16000,
			&models.ModelReasoning{SupportedEfforts: []string{"low", "none"}, DefaultEffort: "low", DefaultEnabled: true}),
	}
	aggregate := FoldMembers(members)
	if !hasAll(aggregate.Capabilities, "tools", "json_mode") {
		t.Fatalf("capabilities: %v", aggregate.Capabilities)
	}
	if !hasAll(aggregate.SupportedParameters, "tools") {
		t.Fatalf("parameters: %v", aggregate.SupportedParameters)
	}
	if !hasAll(aggregate.InputModalities, "text") {
		t.Fatalf("input: %v", aggregate.InputModalities)
	}
	if !hasAll(aggregate.OutputModalities, "text") {
		t.Fatalf("output: %v", aggregate.OutputModalities)
	}
	if aggregate.ContextLength != 50000 || aggregate.MaxCompletionTokens != 8000 {
		t.Fatalf("limits: %d/%d", aggregate.ContextLength, aggregate.MaxCompletionTokens)
	}
	if aggregate.Reasoning == nil || !hasAll(aggregate.Reasoning.SupportedEfforts, "low") {
		t.Fatalf("reasoning: %+v", aggregate.Reasoning)
	}
}

func TestFoldMembers_NoReasoningWhenMemberLacks(t *testing.T) {
	reasoning := &models.ModelReasoning{SupportedEfforts: []string{"low"}}
	members := []models.ModelInfo{
		modelWithCapabilities("a", nil, nil, []string{"text"}, []string{"text"}, 1000, 100, reasoning),
		modelWithCapabilities("b", nil, nil, []string{"text"}, []string{"text"}, 1000, 100, nil),
	}
	if aggregate := FoldMembers(members); aggregate.Reasoning != nil {
		t.Fatalf("reasoning must be nil: %+v", aggregate.Reasoning)
	}
}

func TestFoldMembers_Empty(t *testing.T) {
	aggregate := FoldMembers(nil)
	if len(aggregate.Capabilities) != 0 || aggregate.ContextLength != 0 || aggregate.Reasoning != nil {
		t.Fatalf("zero metadata expected: %+v", aggregate)
	}
}
