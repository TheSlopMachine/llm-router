package luaplugin

import (
	"context"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

const streamPluginSource = `--- @plugin Stream Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @description Stream plugin
--- @allow_host example.com

llm_router.register("stream-type", {
  complete = function(ctx, request)
    return {
      id = "c1", object = "chat.completion", created = 1, model = request.model,
      choices = { { index = 0, message = { role = "assistant", content = "hi" }, finish_reason = "stop" } },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,

  complete_stream = function(ctx, request, emit)
    emit({
      id = "s1", object = "chat.completion.chunk", created = 1, model = request.model,
      choices = { { index = 0, delta = { role = "assistant", content = "he" } } },
    })
    emit({
      id = "s2", object = "chat.completion.chunk", created = 1, model = request.model,
      choices = { { index = 0, delta = { role = "assistant", content = "y" } } },
    })
    -- Usage-only final chunk: empty choices, usage present (OpenAI include_usage shape).
    emit({
      id = "s3", object = "chat.completion.chunk", created = 1, model = request.model,
      choices = {},
      usage = { prompt_tokens = 5, completion_tokens = 6, total_tokens = 11 },
    })
  end,
})
`

func TestCompleteStream_UsageOnlyChunkAllowed(t *testing.T) {
	service := setupService(t)
	if _, err := service.InstallSQLite(context.Background(), []byte(streamPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	var buf strings.Builder
	request := &models.ChatCompletionRequest{
		Model:    "stream-type/model-a",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}
	if err := service.CompleteStream(context.Background(), testMetadata("stream-type", nil, request.Model, nil), request, &buf); err != nil {
		t.Fatalf("complete stream: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, `"total_tokens":11`) {
		t.Fatalf("usage-only chunk dropped: %q", output)
	}
	if !strings.Contains(output, "data: [DONE]") {
		t.Fatalf("missing DONE: %q", output)
	}
}

const badStreamPluginSource = `--- @plugin Bad Stream Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @description Bad stream plugin
--- @allow_host example.com

llm_router.register("bad-stream-type", {
  complete = function(ctx, request)
    return {
      id = "c1", object = "chat.completion", created = 1, model = request.model,
      choices = { { index = 0, message = { role = "assistant", content = "hi" }, finish_reason = "stop" } },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,

  complete_stream = function(ctx, request, emit)
    -- Neither choices nor usage: must be rejected.
    emit({ id = "s1", object = "chat.completion.chunk", created = 1, model = request.model })
  end,
})
`

func TestCompleteStream_EmptyChunkRejected(t *testing.T) {
	service := setupService(t)
	if _, err := service.InstallSQLite(context.Background(), []byte(badStreamPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	var buf strings.Builder
	request := &models.ChatCompletionRequest{
		Model:    "bad-stream-type/model-a",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}
	err := service.CompleteStream(context.Background(), testMetadata("bad-stream-type", nil, request.Model, nil), request, &buf)
	if err == nil {
		t.Fatal("expected error for chunk without choices and usage")
	}
}

const completeOnlyPluginSource = `--- @plugin Complete Only
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @description Plugin without complete_stream
--- @allow_host example.com

llm_router.register("complete-only-type", {
  complete = function(ctx, request)
    return {
      id = "c1", object = "chat.completion", created = 1, model = request.model,
      choices = { { index = 0, message = { role = "assistant", content = "hello" }, finish_reason = "stop" } },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,
})
`

func TestCompleteStream_EmulatesOverCompleteWhenHandlerMissing(t *testing.T) {
	service := setupService(t)
	if _, err := service.InstallSQLite(context.Background(), []byte(completeOnlyPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	var buf strings.Builder
	request := &models.ChatCompletionRequest{
		Model:    "complete-only-type/model-a",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}
	if err := service.CompleteStream(context.Background(), testMetadata("complete-only-type", nil, request.Model, nil), request, &buf); err != nil {
		t.Fatalf("complete stream: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, `"content":"hello"`) {
		t.Fatalf("emulated chunk missing content: %s", output)
	}
	if !strings.HasSuffix(output, "data: [DONE]\n\n") {
		t.Fatalf("emulated stream must end with [DONE]: %s", output)
	}
}
