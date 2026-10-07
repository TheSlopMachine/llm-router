package luaplugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

const streamAPIPluginTemplate = `--- @plugin Stream API Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("sapi-type", {
  proxy_schema = {},
  complete = function(ctx, request)
    local client = llm_router.http_client({})
    local seen = {}
    local proxy_url = (llm_router.proxies.query({})[1] or {}).url
    local resp, err = client:stream({
      method = "GET", url = "http://example.com/sse", proxy_url = proxy_url,
      on_response = function(r)
        if r.status ~= 200 then
          return { message = "upstream status " .. tostring(r.status) .. ": " .. tostring(r.body), code = "server_error", status = 502 }
        end
      end,
      on_line = function(line)
        table.insert(seen, line)
      end,
    })
    if err then return nil, err end
    if resp.status ~= 200 then
      return nil, { message = "bad head", code = "server_error" }
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
	if _, err := svc.Install([]byte(streamAPIPluginTemplate), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	svc.SetProxyQuery(func(pool, country string, limit int) ([]models.ProxyView, error) {
		return []models.ProxyView{{ID: "px-test", URL: proxyURL, Pool: "auto"}}, nil
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

func TestStreamAPI_OnResponseHookTerminal(t *testing.T) {
	svc := setupStreamAPIService(t, streamProxyStub(t, 429, "quota exceeded for today"))
	_, err := sapiComplete(t, svc)
	var perr *models.ProviderError
	if !errors.As(err, &perr) || perr.Code != "server_error" || perr.StatusCode != 502 {
		t.Fatalf("hook must return the terminal table, got %T (%v)", err, err)
	}
	if perr.Message == "" {
		t.Fatal("terminal message must survive")
	}
}

func TestStreamAPI_DefaultNon2xxIsTerminal(t *testing.T) {
	svc := setupService(t)
	src := fmt.Sprintf(`--- @plugin Stream Default
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("sdef-type", {
  proxy_schema = {},
  complete = function(ctx, request)
    local client = llm_router.http_client({})
    local proxy_url = (llm_router.proxies.query({})[1] or {}).url
    local resp, err = client:stream({
      method = "GET", url = "http://example.com/sse", proxy_url = proxy_url,
      on_line = function(line) end,
    })
    if err then return nil, err end
    return nil, { message = "should not happen", code = "server_error" }
  end,
})
`)
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	svc.SetProxyQuery(func(pool, country string, limit int) ([]models.ProxyView, error) {
		return []models.ProxyView{{ID: "px-test", URL: streamProxyStub(t, 500, "boom"), Pool: "auto"}}, nil
	})
	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	_, err := svc.Complete(context.Background(), testMeta("sdef-type", cred, "sdef-type/m", nil),
		&models.ChatCompletionRequest{
			Model:    "sdef-type/m",
			Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
		})
	var perr *models.ProviderError
	if !errors.As(err, &perr) || perr.Code != "server_error" {
		t.Fatalf("default non-2xx must be terminal server_error, got %T (%v)", err, err)
	}
}
