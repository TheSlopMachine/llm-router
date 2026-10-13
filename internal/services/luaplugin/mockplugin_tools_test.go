package luaplugin

import (
	"context"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil/mockplugin"
)

func TestMockPluginTools_Complete(t *testing.T) {
	service := setupService(t)
	fixture := mockplugin.Tools()
	if _, err := service.InstallSQLite(context.Background(), []byte(fixture.Source), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install %s: %v", fixture.TypeKey, err)
	}

	response, err := service.Complete(context.Background(), testMetadata(fixture.TypeKey,
		&models.Credential{ID: "c1", Data: map[string]any{"api_key": "k"}}, models.ModelId(fixture.TypeKey+"/tools"), nil),
		&models.ChatCompletionRequest{
			Model:    models.ModelId(fixture.TypeKey + "/tools"),
			Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
			Tools:    []models.ChatTool{{Type: "function", Function: &models.ChatToolFunction{Name: "get_weather"}}},
		})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(response.Choices) != 1 {
		t.Fatalf("choices: %+v", response.Choices)
	}
	choice := response.Choices[0]
	if choice.FinishReason != "tool_calls" {
		t.Fatalf("finish reason: got %q, want tool_calls", choice.FinishReason)
	}
	if len(choice.Message.ToolCalls) != 1 || choice.Message.ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("tool calls: %+v", choice.Message.ToolCalls)
	}
}
