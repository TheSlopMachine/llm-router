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
--- @router_version 0.0.4
--- @description Stream plugin
--- @allow_host example.com

llm_router.register("stream-type", {
  complete = function(ctx, credential, request)
    return {
      id = "c1", object = "chat.completion", created = 1, model = request.model,
      choices = { { index = 0, message = { role = "assistant", content = "hi" }, finish_reason = "stop" } },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,

  complete_stream = function(ctx, credential, request, emit)
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
	svc := setupService(t)
	if _, err := svc.Install([]byte(streamPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	var buf strings.Builder
	req := &models.ChatCompletionRequest{
		Model:    "stream-type/model-a",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}
	if err := svc.CompleteStream(context.Background(), "stream-type", nil, req, &buf, nil); err != nil {
		t.Fatalf("complete stream: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"total_tokens":11`) {
		t.Fatalf("usage-only chunk dropped: %q", out)
	}
	if !strings.Contains(out, "data: [DONE]") {
		t.Fatalf("missing DONE: %q", out)
	}
}

const badStreamPluginSource = `--- @plugin Bad Stream Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.0.4
--- @description Bad stream plugin
--- @allow_host example.com

llm_router.register("bad-stream-type", {
  complete = function(ctx, credential, request)
    return {
      id = "c1", object = "chat.completion", created = 1, model = request.model,
      choices = { { index = 0, message = { role = "assistant", content = "hi" }, finish_reason = "stop" } },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,

  complete_stream = function(ctx, credential, request, emit)
    -- Neither choices nor usage: must be rejected.
    emit({ id = "s1", object = "chat.completion.chunk", created = 1, model = request.model })
  end,
})
`

func TestCompleteStream_EmptyChunkRejected(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(badStreamPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	var buf strings.Builder
	req := &models.ChatCompletionRequest{
		Model:    "bad-stream-type/model-a",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}
	err := svc.CompleteStream(context.Background(), "bad-stream-type", nil, req, &buf, nil)
	if err == nil {
		t.Fatal("expected error for chunk without choices and usage")
	}
}
