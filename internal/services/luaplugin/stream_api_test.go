package luaplugin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

const streamAPIPluginSource = `--- @plugin Stream API Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.1.1
--- @allow_host example.com

llm_router.register("sapi-type", {
  complete = function(ctx, credential, request)
    local client = llm_router.http_client({})
    local seen = {}
    local resp, err = client:stream({
      method = "GET", url = "http://example.com/sse",
      on_response = function(r)
        if r.status ~= 200 then
          return llm_router.classify_error({ status = r.status, headers = r.headers, body = r.body })
        end
      end,
      on_line = function(line)
        table.insert(seen, line)
      end,
    })
    if err then return nil, err end
    if resp.status ~= 200 then
      return nil, { type = "upstream", message = "bad head" }
    end
    return {
      id = "s", object = "chat.completion", created = 1, model = request.model,
      choices = { { index = 0, message = { role = "assistant", content = table.concat(seen, "|") }, finish_reason = "stop" } },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,
})
`

// streamProxyStub answers every absolute-form GET with the fixed status and
// body: traffic routed through the proxy observes it, direct dials never do.
func streamProxyStub(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func setupStreamAPIService(t *testing.T, proxyURL string) *Service {
	t.Helper()
	svc := setupService(t)
	if _, err := svc.Install([]byte(streamAPIPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	svc.SetProxyResolver(func(_ context.Context, _ *PluginRecord, _ map[string]any) ([]ProxyPick, error) {
		return []ProxyPick{{ID: "px-test", URL: proxyURL}}, nil
	})
	return svc
}

func sapiComplete(t *testing.T, svc *Service) (*models.ChatCompletionResponse, error) {
	t.Helper()
	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	return svc.Complete(context.Background(), testMeta("sapi-type", cred, "sapi-type/m", nil),
		&models.ChatCompletionRequest{
			Model:    "sapi-type/m",
			Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
		})
}

func TestStreamAPI_SuccessReturnsResp(t *testing.T) {
	svc := setupStreamAPIService(t, streamProxyStub(t, 200, "data: one\n\ndata: two\n\n"))
	resp, err := sapiComplete(t, svc)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if got := resp.Choices[0].Message.TextContent(); got != "data: one||data: two|" {
		t.Fatalf("lines: %q", got)
	}
}

func TestStreamAPI_OnResponseHookClassifies(t *testing.T) {
	// Quota wording rides the error body: only a hook receiving the body
	// classifies quota_exceeded instead of rate_limit.
	svc := setupStreamAPIService(t, streamProxyStub(t, 429, "quota exceeded for today"))
	_, err := sapiComplete(t, svc)
	var perr *models.ProviderError
	if !errors.As(err, &perr) || perr.Type != models.ErrorTypeQuotaExceeded {
		t.Fatalf("hook must classify quota from the error body, got %T (%v)", err, err)
	}
}

func TestStreamAPI_DefaultNon2xxIsUpstream(t *testing.T) {
	svc := setupService(t)
	src := `--- @plugin Stream Default
--- @author tester
--- @version 1.0.0
--- @router_version 0.1.1
--- @allow_host example.com

llm_router.register("sdef-type", {
  complete = function(ctx, credential, request)
    local client = llm_router.http_client({})
    local resp, err = client:stream({
      method = "GET", url = "http://example.com/sse",
      on_line = function(line) end,
    })
    if err then return nil, err end
    return nil, { type = "upstream", message = "should not happen" }
  end,
})
`
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	svc.SetProxyResolver(func(_ context.Context, _ *PluginRecord, _ map[string]any) ([]ProxyPick, error) {
		return []ProxyPick{{ID: "px-test", URL: streamProxyStub(t, 500, "boom")}}, nil
	})
	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	_, err := svc.Complete(context.Background(), testMeta("sdef-type", cred, "sdef-type/m", nil),
		&models.ChatCompletionRequest{
			Model:    "sdef-type/m",
			Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
		})
	var perr *models.ProviderError
	if !errors.As(err, &perr) || perr.Type != models.ErrorTypeUpstream {
		t.Fatalf("default non-2xx must be upstream, got %T (%v)", err, err)
	}
}
