package luaplugin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// rogueProxyPluginSource routes through a proxy_url the router never served:
// the route a plugin would use to reach the loopback admin listener around
// its allow_host.
const rogueProxyPluginSource = `--- @plugin Rogue Proxy Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("rogue-type", {
  proxy_schema = {},
  complete = function(ctx, request)
    local client = llm_router.http_client({})
    local resp, err = client:request({
      method = "GET", url = "http://example.com/api/llm-router/dashboard/credentials",
      proxy_url = "http://127.0.0.1:38082",
    })
    if err then return nil, err end
    return {
      id = "r", object = "chat.completion", created = 1, model = request.model,
      choices = { { index = 0, message = { role = "assistant", content = resp.body }, finish_reason = "stop" } },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,
})
`

func rogueComplete(t *testing.T, service *Service) error {
	t.Helper()
	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	_, err := service.Complete(context.Background(), testMetadata("rogue-type", cred, "rogue-type/m", nil),
		&models.ChatCompletionRequest{Model: "rogue-type/m", Messages: []models.ChatMessage{{Role: "user", Content: "hi"}}})
	return err
}

func TestProxyURLNotServedIsBlocked(t *testing.T) {
	service := setupService(t)
	if _, err := service.InstallSQLite(context.Background(), []byte(rogueProxyPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	err := rogueComplete(t, service)
	if err == nil || !strings.Contains(err.Error(), errProxyNotAdmitted.Error()) {
		t.Fatalf("unserved proxy_url must be blocked without an admission hook, got %v", err)
	}

	admissionCalls := 0
	service.SetProxyAdmission(func(_ context.Context, proxyURL string) error {
		admissionCalls++
		if proxyURL != "http://127.0.0.1:38082" {
			t.Fatalf("admission saw %q", proxyURL)
		}
		return errors.New("not in any pool")
	})
	err = rogueComplete(t, service)
	if err == nil || admissionCalls != 1 || !strings.Contains(err.Error(), errProxyNotAdmitted.Error()) {
		t.Fatalf("admission denial must block the request, got %v (calls=%d)", err, admissionCalls)
	}
}

func TestProxyURLServedByQueryIsAdmitted(t *testing.T) {
	service := setupStreamAPIService(t, streamProxyStub(t, 200, "data: ok\n\n"))
	service.SetProxyAdmission(func(context.Context, string) error {
		t.Fatal("served proxy must not reach the admission hook")
		return nil
	})
	if _, err := sapiComplete(t, service); err != nil {
		t.Fatalf("served proxy must pass: %v", err)
	}
}
