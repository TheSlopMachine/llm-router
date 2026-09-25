package luaplugin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

const embedPluginSource = `--- @plugin Embed Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.1.1
--- @description Embed test plugin
--- @allow_host example.com

llm_router.register("emb-type", {
  complete = function(ctx, credential, request)
    return {
      id = "chatcmpl-emb",
      object = "chat.completion",
      created = 1700000000,
      model = request.model,
      choices = {
        { index = 0, message = { role = "assistant", content = "hi" }, finish_reason = "stop" },
      },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,

  embed = function(ctx, credential, request)
    local data = {}
    for i, text in ipairs(request.input) do
      table.insert(data, { embedding = { 0.1 * i, 0.2, -0.3 } })
    end
    return { data = data, usage = { prompt_tokens = #request.input, total_tokens = #request.input } }
  end,
})
`

func TestEmbed_Success(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(embedPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !svc.HasHandler("emb-type", "embed") {
		t.Fatal("embed handler must be registered")
	}
	resp, err := svc.Embed(context.Background(), testMeta("emb-type",
		&models.Credential{ID: "c1"}, "emb-type/gemini-embedding-001", nil),
		&models.EmbeddingsRequest{
			Model: "emb-type/gemini-embedding-001",
			Input: []string{"hello", "world"},
		})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("data len: %d", len(resp.Data))
	}
	for i, e := range resp.Data {
		if e.Index != i {
			t.Fatalf("index %d: got %d", i, e.Index)
		}
		if len(e.Values) != 3 {
			t.Fatalf("values len %d: %v", i, e.Values)
		}
	}
	if resp.Data[1].Values[0] != 0.2 {
		t.Fatalf("second vector first value: %v", resp.Data[1].Values[0])
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 2 {
		t.Fatalf("usage: %+v", resp.Usage)
	}
}

func TestEmbed_HandlerNotFound(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(testPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Embed(context.Background(), testMeta("test-type",
		&models.Credential{ID: "c1"}, "test-type/model-a", nil),
		&models.EmbeddingsRequest{Model: "test-type/model-a", Input: []string{"x"}})
	if !errors.Is(err, ErrHandlerNotFound) {
		t.Fatalf("expected ErrHandlerNotFound, got %v", err)
	}
}

func TestEmbed_ContractError(t *testing.T) {
	svc := setupService(t)
	src := strings.Replace(embedPluginSource, `local data = {}`,
		`do return nil, { type = "auth", message = "bad key" } end
    local data = {}`, 1)
	src = strings.Replace(src, "Embed Plugin", "Embed Err", 1)
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Embed(context.Background(), testMeta("emb-type",
		&models.Credential{ID: "c1"}, "emb-type/m", nil),
		&models.EmbeddingsRequest{Model: "emb-type/m", Input: []string{"x"}})
	perr, ok := err.(*models.ProviderError)
	if !ok || perr.Type != models.ErrorTypeAuth {
		t.Fatalf("expected auth ProviderError, got %T (%v)", err, err)
	}
}

func TestEmbed_ShortResultIsCrash(t *testing.T) {
	svc := setupService(t)
	src := strings.Replace(embedPluginSource, `for i, text in ipairs(request.input) do`,
		`for i, text in ipairs({request.input[1]}) do`, 1)
	src = strings.Replace(src, "Embed Plugin", "Embed Short", 1)
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Embed(context.Background(), testMeta("emb-type",
		&models.Credential{ID: "c1"}, "emb-type/m", nil),
		&models.EmbeddingsRequest{Model: "emb-type/m", Input: []string{"a", "b"}})
	var perr *models.PluginInternalError
	if !errors.As(err, &perr) || !strings.Contains(perr.Cause, "does not match input length") {
		t.Fatalf("expected PluginInternalError about length mismatch, got %v", err)
	}
}
