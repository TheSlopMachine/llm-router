package luaplugin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
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
--- @router_version 0.0.4
--- @allow_host example.com

llm_router.register("proxy-fetch-type", {
  complete = function(ctx, credential, request)
    local client = llm_router.create_http_client({ timeout_ms = 5000 })
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
	svc.SetProxyResolver(func(_ context.Context, rec *PluginRecord, _ map[string]any) ([]ProxyPick, error) {
		resolved++
		return []ProxyPick{{ID: "px-test", URL: proxyURL}}, nil
	})

	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	resp, err := svc.Complete(t.Context(), "proxy-fetch-type", cred, &models.ChatCompletionRequest{
		Model:    "test/proxy-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
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
	svc.SetProxyResolver(func(_ context.Context, _ *PluginRecord, _ map[string]any) ([]ProxyPick, error) {
		return []ProxyPick{{ID: "px-dead", URL: deadURL}, {ID: "px-live", URL: liveURL}}, nil
	})

	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	resp, err := svc.Complete(t.Context(), "proxy-fetch-type", cred, &models.ChatCompletionRequest{
		Model:    "test/proxy-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if err != nil {
		t.Fatalf("rotation should recover: %v", err)
	}
	if got := resp.Choices[0].Message.TextContent(); !strings.Contains(got, "via-second-proxy") {
		t.Fatalf("did not rotate to live proxy: %q", got)
	}
}

func TestComplete_ProxyExhaustedSurfacesError(t *testing.T) {
	svc := setupService(t)
	installProxyFetchPlugin(t, svc)

	// The only pooled proxy refuses connections: the error must surface,
	// never a silent direct attempt.
	svc.SetProxyResolver(func(_ context.Context, _ *PluginRecord, _ map[string]any) ([]ProxyPick, error) {
		return []ProxyPick{{ID: "px-dead", URL: "http://127.0.0.1:1"}}, nil
	})

	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	_, err := svc.Complete(t.Context(), "proxy-fetch-type", cred, &models.ChatCompletionRequest{
		Model:    "test/proxy-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if err == nil {
		t.Fatal("exhausted proxy list must surface an error")
	}
}

const proxyRateLimitPluginSource = `--- @plugin Proxy Rate Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.0.4
--- @allow_host example.com

llm_router.register("proxy-rate-type", {
  complete = function(ctx, credential, request)
    local client = llm_router.create_http_client({ timeout_ms = 5000 })
    local resp, req_err = client:request({ method = "GET", url = "http://example.com/probe" })
    if req_err ~= nil then
      return nil, { type = "upstream", message = req_err.message }
    end
    return nil, { type = "rate_limit", message = "slow down", retry_after = 1700000060 }
  end,
})
`

const proxyGeoPluginSource = `--- @plugin Proxy Geo Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.0.4
--- @allow_host example.com

llm_router.register("proxy-geo-type", {
  complete = function(ctx, credential, request)
    local client = llm_router.create_http_client({ timeout_ms = 5000 })
    local resp, req_err = client:request({ method = "GET", url = "http://example.com/probe" })
    if req_err ~= nil then
      return nil, { type = "upstream", message = req_err.message }
    end
    return nil, { type = "geo", message = "region locked" }
  end,
})
`

func TestComplete_RateLimitReportsPairEvent(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(proxyRateLimitPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	proxyURL := markerProxy(t, "x")
	svc.SetProxyResolver(func(_ context.Context, _ *PluginRecord, _ map[string]any) ([]ProxyPick, error) {
		return []ProxyPick{{ID: "px-1", URL: proxyURL}}, nil
	})
	var events []ProxyEvent
	svc.SetProxyEventReporter(func(ev ProxyEvent) {
		events = append(events, ev)
	})

	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	_, err := svc.Complete(t.Context(), "proxy-rate-type", cred, &models.ChatCompletionRequest{
		Model:    "test/rate-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if err == nil {
		t.Fatal("rate limit must surface an error")
	}
	if len(events) != 1 || !events[0].RateLimited || events[0].ProxyID != "px-1" {
		t.Fatalf("rate limit pair event missing: %+v", events)
	}
	if events[0].ResetsAt.Unix() != 1700000060 {
		t.Fatalf("retry_after not honored: %+v", events[0])
	}
}

func TestComplete_GeoReportsPairBlock(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(proxyGeoPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	proxyURL := markerProxy(t, "ok-region")
	svc.SetProxyResolver(func(_ context.Context, _ *PluginRecord, _ map[string]any) ([]ProxyPick, error) {
		return []ProxyPick{{ID: "px-1", URL: proxyURL}}, nil
	})
	var events []ProxyEvent
	svc.SetProxyEventReporter(func(ev ProxyEvent) {
		events = append(events, ev)
	})

	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	_, err := svc.Complete(t.Context(), "proxy-geo-type", cred, &models.ChatCompletionRequest{
		Model:    "test/geo-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if err == nil {
		t.Fatal("geo error must surface")
	}
	var perr *models.ProviderError
	if !errors.As(err, &perr) || perr.Type != models.ErrorTypeGeo {
		t.Fatalf("geo error must stay a geo provider error: %v", err)
	}
	if len(events) != 1 || !events[0].Blocked || events[0].ProxyID != "px-1" {
		t.Fatalf("geo pair block missing: %+v", events)
	}
	if events[0].BlockReason != "region locked" {
		t.Fatalf("block reason not carried: %+v", events[0])
	}
}

func TestComplete_RateLimitDefaultResetsInMinute(t *testing.T) {
	svc := setupService(t)
	src := strings.Replace(proxyRateLimitPluginSource, "proxy-rate-type", "proxy-rate-dflt", 1)
	src = strings.Replace(src, `, retry_after = 1700000060`, ``, 1)
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	proxyURL := markerProxy(t, "x")
	svc.SetProxyResolver(func(_ context.Context, _ *PluginRecord, _ map[string]any) ([]ProxyPick, error) {
		return []ProxyPick{{ID: "px-1", URL: proxyURL}}, nil
	})
	var events []ProxyEvent
	svc.SetProxyEventReporter(func(ev ProxyEvent) {
		events = append(events, ev)
	})
	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	// The rate_limit fixture without retry_after defaults in asProviderError;
	// quota_exceeded defaults to now+60s at the contract layer.
	before := time.Now()
	_, _ = svc.Complete(t.Context(), "proxy-rate-dflt", cred, &models.ChatCompletionRequest{
		Model:    "test/rate-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if len(events) != 1 || !events[0].RateLimited {
		t.Fatalf("rate limit pair event missing: %+v", events)
	}
	if events[0].ResetsAt.Before(before) {
		t.Fatalf("default reset must be in the future: %+v", events[0])
	}
}

func TestDoWithProxyRotation_BadTransportNeverGoesDirect(t *testing.T) {
	// Unbuildable proxy URL: rotation has nothing to advance to, so the
	// build error surfaces. No network access happens either way.
	ctx := &execContext{
		proxyResolver: func(_ context.Context, _ *PluginRecord, _ map[string]any) ([]ProxyPick, error) {
			return []ProxyPick{{ID: "px-bogus", URL: "bogus-scheme://example.com:8080"}}, nil
		},
	}
	c := &pluginHTTPClient{ctx: ctx, guard: newSSRFGuard([]string{"example.com"})}
	req, _ := http.NewRequest("GET", "http://example.com/", nil)
	if _, _, _, err := c.doWithProxyRotation(req); err == nil {
		t.Fatal("expected loud error for unbuildable proxy transport")
	}
}

func TestExecContext_RotationBounds(t *testing.T) {
	calls := 0
	ctx := &execContext{
		proxyResolver: func(_ context.Context, _ *PluginRecord, _ map[string]any) ([]ProxyPick, error) {
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
		proxyResolver: func(context.Context, *PluginRecord, map[string]any) ([]ProxyPick, error) {
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
