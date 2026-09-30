package luaplugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
	"github.com/TheSlopMachine/llm-router/internal/streamgate"
)

// markerProxy answers every absolute-form GET with a fixed marker body.
// Only traffic routed through the proxy can observe the marker.
func markerProxy(t *testing.T, marker string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, marker)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

const proxyFetchPluginSource = `--- @plugin Proxy Fetch Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.3.0
--- @allow_host example.com

llm_router.register("proxy-fetch-type", {
  complete = function(ctx, credential, request)
    local client = llm_router.http_client({ timeout_ms = 5000 })
    local resp, req_err = client:request({ method = "GET", url = "http://example.com/probe" })
    if req_err ~= nil then
      return nil, { type = "upstream", message = req_err.message }
    end
    return {
      id = "chatcmpl-proxy",
      object = "chat.completion",
      created = 1700000000,
      model = request.model,
      choices = {
        { index = 0, message = { role = "assistant", content = resp.body }, finish_reason = "stop" },
      },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,
})
`

func installProxyFetchPlugin(t *testing.T, svc *Service) {
	t.Helper()
	if _, err := svc.Install([]byte(proxyFetchPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
}

func TestComplete_RoutesThroughProxy(t *testing.T) {
	svc := setupService(t)
	installProxyFetchPlugin(t, svc)
	proxyURL := markerProxy(t, "via-proxy-marker")

	var resolved int
	svc.SetProxyResolver(func(_ context.Context, rec *PluginRecord, _ map[string]any, _ exhausted.Segments) ([]ProxyPick, error) {
		resolved++
		return []ProxyPick{{ID: "px-test", URL: proxyURL}}, nil
	})

	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	resp, err := svc.Complete(t.Context(), testMeta("proxy-fetch-type", cred, "test/proxy-model", nil), &models.ChatCompletionRequest{
		Model:    "test/proxy-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resolved == 0 {
		t.Fatal("proxy resolver never consulted")
	}
	got := resp.Choices[0].Message.TextContent()
	if !strings.Contains(got, "via-proxy-marker") {
		t.Fatalf("response did not come through the proxy: %q", got)
	}
}

func TestComplete_RotatesToNextProxyOnFailure(t *testing.T) {
	svc := setupService(t)
	installProxyFetchPlugin(t, svc)
	liveURL := markerProxy(t, "via-second-proxy")
	deadURL := "http://127.0.0.1:1"

	// Dead first, live second: one ordered list, failover walks it.
	svc.SetProxyResolver(func(_ context.Context, _ *PluginRecord, _ map[string]any, _ exhausted.Segments) ([]ProxyPick, error) {
		return []ProxyPick{{ID: "px-dead", URL: deadURL}, {ID: "px-live", URL: liveURL}}, nil
	})

	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	resp, err := svc.Complete(t.Context(), testMeta("proxy-fetch-type", cred, "test/proxy-model", nil), &models.ChatCompletionRequest{
		Model:    "test/proxy-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("rotation should recover: %v", err)
	}
	if got := resp.Choices[0].Message.TextContent(); !strings.Contains(got, "via-second-proxy") {
		t.Fatalf("did not rotate to live proxy: %q", got)
	}
}

const proxyLimitRetryPluginSource = `--- @plugin Proxy Limit Retry
--- @author tester
--- @version 1.0.0
--- @router_version 0.3.4
--- @allow_host example.com

llm_router.register("proxy-limit-type", {
  complete = function(ctx, credential, request)
    local client = llm_router.http_client({ timeout_ms = 5000 })
    local resp, req_err = client:request({ method = "GET", url = "http://example.com/probe" })
    if req_err ~= nil then
      return nil, { type = "upstream", message = req_err.message }
    end
    if resp.status == 429 then
      local scope = request.user == "account" and { "account" } or { "proxy" }
      local error_type = request.user == "quota" and "quota_exceeded" or "rate_limit"
      return nil, {
        type = error_type, message = "exit limit", retry_after = os.time() + 60,
        scope = scope,
      }
    end
    return {
      id = "chatcmpl-proxy-limit",
      object = "chat.completion",
      created = 1700000000,
      model = request.model,
      choices = {
        { index = 0, message = { role = "assistant", content = resp.body }, finish_reason = "stop" },
      },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,
})
`

type proxyLimitRecorder struct {
	proxyIDs []string
}

func (r *proxyLimitRecorder) MarkLimit(proxyID, _ string, _ time.Time, _ string) error {
	r.proxyIDs = append(r.proxyIDs, proxyID)
	return nil
}

func installProxyLimitRetryPlugin(t *testing.T, svc *Service) {
	t.Helper()
	if _, err := svc.Install([]byte(proxyLimitRetryPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
}

func countedStatusProxy(t *testing.T, status int, body string, hits *atomic.Int32) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestCompletePool_RetriesProxyScopedQuotaOnAlternateProxy(t *testing.T) {
	svc := setupService(t)
	installProxyLimitRetryPlugin(t, svc)
	var limitedHits, alternateHits atomic.Int32
	limitedURL := countedStatusProxy(t, http.StatusTooManyRequests, "limit", &limitedHits)
	alternateURL := countedStatusProxy(t, http.StatusOK, "alternate response", &alternateHits)
	svc.SetProxyResolver(func(context.Context, *PluginRecord, map[string]any, exhausted.Segments) ([]ProxyPick, error) {
		return []ProxyPick{{ID: "px-limited", URL: limitedURL}, {ID: "px-alternate", URL: alternateURL}}, nil
	})
	recorder := &proxyLimitRecorder{}
	svc.SetProxyLimitStore(recorder)

	mode := "quota"
	req := &models.ChatCompletionRequest{
		Model: "proxy-limit-type/m", User: &mode,
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}
	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	resp, route, err := svc.CompletePool(t.Context(), testMeta("proxy-limit-type", nil, req.Model, nil), []*models.Credential{cred}, req)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got := resp.Choices[0].Message.TextContent(); got != "alternate response" {
		t.Fatalf("response: got %q", got)
	}
	if route == "" || limitedHits.Load() != 1 || alternateHits.Load() != 1 {
		t.Fatalf("routes: last=%q limited=%d alternate=%d", route, limitedHits.Load(), alternateHits.Load())
	}
	if len(recorder.proxyIDs) != 1 || recorder.proxyIDs[0] != "px-limited" {
		t.Fatalf("limited proxies: %v", recorder.proxyIDs)
	}
}

func TestCompletePool_NoAlternateProxyReturnsOriginalLimit(t *testing.T) {
	svc := setupService(t)
	installProxyLimitRetryPlugin(t, svc)
	var limitedHits atomic.Int32
	limitedURL := countedStatusProxy(t, http.StatusTooManyRequests, "limit", &limitedHits)
	svc.SetProxyResolver(func(context.Context, *PluginRecord, map[string]any, exhausted.Segments) ([]ProxyPick, error) {
		return []ProxyPick{{ID: "px-limited", URL: limitedURL}}, nil
	})
	svc.SetProxyLimitStore(&proxyLimitRecorder{})

	mode := "quota"
	req := &models.ChatCompletionRequest{
		Model: "proxy-limit-type/m", User: &mode,
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}
	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	_, _, err := svc.CompletePool(t.Context(), testMeta("proxy-limit-type", nil, req.Model, nil), []*models.Credential{cred}, req)
	if !isProviderType(err, models.ErrorTypeQuotaExceeded) {
		t.Fatalf("expected original quota error, got %T (%v)", err, err)
	}
	if got := limitedHits.Load(); got != 1 {
		t.Fatalf("limited proxy was called %d times", got)
	}
}

func TestCompletePool_DoesNotRetryAccountScopedLimit(t *testing.T) {
	svc := setupService(t)
	installProxyLimitRetryPlugin(t, svc)
	var firstHits, secondHits atomic.Int32
	firstURL := countedStatusProxy(t, http.StatusTooManyRequests, "limit", &firstHits)
	secondURL := countedStatusProxy(t, http.StatusOK, "alternate response", &secondHits)
	svc.SetProxyResolver(func(context.Context, *PluginRecord, map[string]any, exhausted.Segments) ([]ProxyPick, error) {
		return []ProxyPick{{ID: "px-first", URL: firstURL}, {ID: "px-second", URL: secondURL}}, nil
	})

	mode := "account"
	req := &models.ChatCompletionRequest{
		Model: "proxy-limit-type/m", User: &mode,
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}
	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	_, _, err := svc.CompletePool(t.Context(), testMeta("proxy-limit-type", nil, req.Model, nil), []*models.Credential{cred}, req)
	if !isProviderType(err, models.ErrorTypeRateLimit) {
		t.Fatalf("expected account-scoped rate limit, got %T (%v)", err, err)
	}
	if firstHits.Load() != 1 || secondHits.Load() != 0 {
		t.Fatalf("account-scoped request used proxies %d and %d times", firstHits.Load(), secondHits.Load())
	}
}

func TestComplete_ProxyExhaustedSurfacesError(t *testing.T) {
	svc := setupService(t)
	installProxyFetchPlugin(t, svc)

	// The only pooled proxy refuses connections: the error must surface,
	// never a silent direct attempt.
	svc.SetProxyResolver(func(_ context.Context, _ *PluginRecord, _ map[string]any, _ exhausted.Segments) ([]ProxyPick, error) {
		return []ProxyPick{{ID: "px-dead", URL: "http://127.0.0.1:1"}}, nil
	})

	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	_, err := svc.Complete(t.Context(), testMeta("proxy-fetch-type", cred, "test/proxy-model", nil), &models.ChatCompletionRequest{
		Model:    "test/proxy-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("exhausted proxy list must surface an error")
	}
}

func TestDoWithProxyRotation_BadTransportNeverGoesDirect(t *testing.T) {
	// Unbuildable proxy URL: rotation has nothing to advance to, so the
	// build error surfaces. No network access happens either way.
	ctx := &execContext{
		proxyResolver: func(_ context.Context, _ *PluginRecord, _ map[string]any, _ exhausted.Segments) ([]ProxyPick, error) {
			return []ProxyPick{{ID: "px-bogus", URL: "bogus-scheme://example.com:8080"}}, nil
		},
	}
	c := &pluginHTTPClient{ctx: ctx, guard: newSSRFGuard([]string{"example.com"})}
	req, _ := http.NewRequest("GET", "http://example.com/", nil)
	if _, _, _, _, err := c.doWithProxyRotation(req); err == nil {
		t.Fatal("expected loud error for unbuildable proxy transport")
	}
}

func TestExecContext_RotationBounds(t *testing.T) {
	calls := 0
	ctx := &execContext{
		proxyResolver: func(_ context.Context, _ *PluginRecord, _ map[string]any, _ exhausted.Segments) ([]ProxyPick, error) {
			calls++
			return []ProxyPick{{ID: "px-1", URL: "http://10.9.9.9:8080"}}, nil
		},
	}
	if err := ctx.beginRequest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ctx.proxyURL == "" {
		t.Fatal("route not resolved")
	}
	// Single pick: rotation stops, no infinite loop, one resolution.
	if ctx.rotateProxy() {
		t.Fatal("rotation must stop at the end of the pick list")
	}
	if calls != 1 {
		t.Fatalf("expected 1 resolution, got %d", calls)
	}
}

func TestBeginRequest_ResolverErrorIsLoud(t *testing.T) {
	// No picks and no direct fallback: a settled empty pool surfaces
	// the resolver error instead of silently going direct.
	ctx := &execContext{
		proxyResolver: func(context.Context, *PluginRecord, map[string]any, exhausted.Segments) ([]ProxyPick, error) {
			return nil, errors.New("proxypool: no usable proxy")
		},
	}
	if err := ctx.beginRequest(context.Background()); err == nil {
		t.Fatal("expected loud resolver error, got nil")
	}
	if len(ctx.proxyPicks) != 0 || ctx.proxyURL != "" {
		t.Fatal("failed resolution must leave the route empty")
	}
}

func TestRedactedProxyHostPort(t *testing.T) {
	if got := redactedProxyHostPort("", "px-1"); got != "" {
		t.Fatalf("direct: got %q want empty", got)
	}
	got := redactedProxyHostPort("http://user:pass@proxy.example:8080", "px-1")
	if got != "proxy.example:8080" {
		t.Fatalf("redacted: got %q want %q", got, "proxy.example:8080")
	}
	if strings.Contains(got, "user") || strings.Contains(got, "pass") {
		t.Fatalf("credentials leaked in %q", got)
	}
	if got := redactedProxyHostPort(":://bad", "px-fallback"); got != "px-fallback" {
		t.Fatalf("unparseable: got %q want fallback", got)
	}
	if got := (&execContext{}).proxyDisplay(); got != "" {
		t.Fatalf("empty ctx: got %q want empty", got)
	}
	var nilCtx *execContext
	if got := nilCtx.proxyDisplay(); got != "" {
		t.Fatalf("nil ctx: got %q want empty", got)
	}
}

func TestCompleteRouted_ReportsProxyHostPort(t *testing.T) {
	svc := setupService(t)
	installProxyFetchPlugin(t, svc)
	proxyURL := markerProxy(t, "via-proxy-marker")
	svc.SetProxyResolver(func(_ context.Context, rec *PluginRecord, _ map[string]any, _ exhausted.Segments) ([]ProxyPick, error) {
		return []ProxyPick{{ID: "px-test", URL: proxyURL}}, nil
	})
	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	_, proxy, err := svc.CompleteRouted(t.Context(), testMeta("proxy-fetch-type", cred, "test/proxy-model", nil), &models.ChatCompletionRequest{
		Model:    "test/proxy-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if proxy == "" {
		t.Fatal("expected proxy host:port, got empty")
	}
	if strings.Contains(proxy, "user") || strings.Contains(proxy, "pass") {
		t.Fatalf("credentials leaked in %q", proxy)
	}
}

func TestCompleteRouted_DirectOmitsProxy(t *testing.T) {
	svc := setupService(t)
	installProxyFetchPlugin(t, svc)
	svc.SetProxyResolver(func(_ context.Context, _ *PluginRecord, _ map[string]any, _ exhausted.Segments) ([]ProxyPick, error) {
		return nil, errors.New("proxypool: no usable proxy")
	})
	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	// Resolver failure leaves the route empty: no proxy is reported even
	// though the call fails.
	_, proxy, _ := svc.CompleteRouted(t.Context(), testMeta("proxy-fetch-type", cred, "test/proxy-model", nil), &models.ChatCompletionRequest{
		Model:    "test/proxy-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if proxy != "" {
		t.Fatalf("direct: got %q want empty", proxy)
	}
}

func TestRunRoutedRetries_ProxyLimitStopsAtStreamCommit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		write     bool
		wantCalls int
	}{
		{name: "before first byte", wantCalls: maxProxyScopedAttempts},
		{name: "after first byte", write: true, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := streamgate.New(&bytes.Buffer{})
			calls := 0
			_, route, err := runRoutedRetries(context.Background(), &Service{}, HandlerMeta{}, models.GeoConfig{}, writer,
				func(context.Context) (int, string, error) {
					calls++
					if tc.write {
						if _, writeErr := io.WriteString(writer, "data: chunk\n\n"); writeErr != nil {
							t.Fatalf("write: %v", writeErr)
						}
					}
					return 0, fmt.Sprintf("proxy-%d:8080", calls), &models.ProviderError{
						Type: models.ErrorTypeRateLimit, Scope: []string{models.ExhaustedScopeProxy},
					}
				})
			if !isProviderType(err, models.ErrorTypeRateLimit) {
				t.Fatalf("expected rate limit, got %T (%v)", err, err)
			}
			if calls != tc.wantCalls {
				t.Fatalf("attempts: got %d want %d", calls, tc.wantCalls)
			}
			if want := fmt.Sprintf("proxy-%d:8080", calls); route != want {
				t.Fatalf("last route: got %q want %q", route, want)
			}
		})
	}
}
