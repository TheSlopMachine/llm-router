package luaplugin

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	svc.SetProxyResolver(func(rec *PluginRecord, _ map[string]any) (string, string, error) {
		resolved++
		return "px-test", proxyURL, nil
	})
	var outcomes []string
	svc.SetProxyOutcomeReporter(func(proxyID, _ string, ok bool, _ int64) {
		if ok {
			outcomes = append(outcomes, proxyID)
		}
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
	if len(outcomes) == 0 {
		t.Fatal("successful proxied request never reported")
	}
}

func TestComplete_RotatesToNextProxyOnFailure(t *testing.T) {
	svc := setupService(t)
	installProxyFetchPlugin(t, svc)
	liveURL := markerProxy(t, "via-second-proxy")
	deadURL := "http://127.0.0.1:1"

	// Dead first, live second: the resolver answers every resolution with
	// the next untried proxy, mimicking pool demotion.
	queue := []struct{ id, url string }{{"px-dead", deadURL}, {"px-live", liveURL}}
	svc.SetProxyResolver(func(_ *PluginRecord, _ map[string]any) (string, string, error) {
		next := queue[0]
		if len(queue) > 1 {
			queue = queue[1:]
		}
		return next.id, next.url, nil
	})
	var failed []string
	svc.SetProxyOutcomeReporter(func(proxyID, _ string, ok bool, _ int64) {
		if !ok {
			failed = append(failed, proxyID)
		}
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
	if len(failed) == 0 || failed[0] != "px-dead" {
		t.Fatalf("dead proxy failure never reported: %v", failed)
	}
}

func TestComplete_ProxyExhaustedSurfacesError(t *testing.T) {
	svc := setupService(t)
	installProxyFetchPlugin(t, svc)

	// The only pooled proxy refuses connections: the error must surface,
	// never a silent direct attempt.
	resolutions := 0
	svc.SetProxyResolver(func(_ *PluginRecord, _ map[string]any) (string, string, error) {
		resolutions++
		return "px-dead", "http://127.0.0.1:1", nil
	})

	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	resp, err := svc.Complete(t.Context(), "proxy-fetch-type", cred, &models.ChatCompletionRequest{
		Model:    "test/proxy-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if err == nil {
		t.Fatalf("exhausted proxy list must surface an error, got content %q (resolutions=%d)", resp.Choices[0].Message.TextContent(), resolutions)
	}
}

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
    if string.find(resp.body, "wrong-region", 1, true) then
      return nil, { type = "geo", message = "region locked" }
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

func installProxyGeoPlugin(t *testing.T, svc *Service) {
	t.Helper()
	if _, err := svc.Install([]byte(proxyGeoPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
}

func TestComplete_GeoRotatesSilently(t *testing.T) {
	svc := setupService(t)
	installProxyGeoPlugin(t, svc)
	lockedURL := markerProxy(t, "wrong-region")
	okURL := markerProxy(t, "right-region")

	queue := []struct{ id, url string }{{"px-locked", lockedURL}, {"px-ok", okURL}}
	svc.SetProxyResolver(func(_ *PluginRecord, _ map[string]any) (string, string, error) {
		next := queue[0]
		if len(queue) > 1 {
			queue = queue[1:]
		}
		return next.id, next.url, nil
	})
	var bad []string
	svc.SetProxyOutcomeReporter(func(proxyID, _ string, ok bool, _ int64) {
		if !ok {
			bad = append(bad, proxyID)
		}
	})

	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	resp, err := svc.Complete(t.Context(), "proxy-geo-type", cred, &models.ChatCompletionRequest{
		Model:    "test/geo-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if err != nil {
		t.Fatalf("geo rotation must recover silently: %v", err)
	}
	if got := resp.Choices[0].Message.TextContent(); !strings.Contains(got, "right-region") {
		t.Fatalf("did not rotate past the locked proxy: %q", got)
	}
	if len(bad) == 0 || bad[0] != "px-locked" {
		t.Fatalf("locked proxy never marked bad for the provider: %v", bad)
	}
}

func TestComplete_GeoExhaustionSynthesizesError(t *testing.T) {
	svc := setupService(t)
	installProxyGeoPlugin(t, svc)
	lockedURL := markerProxy(t, "wrong-region")

	svc.SetProxyResolver(func(_ *PluginRecord, _ map[string]any) (string, string, error) {
		return "px-locked", lockedURL, nil
	})

	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	_, err := svc.Complete(t.Context(), "proxy-geo-type", cred, &models.ChatCompletionRequest{
		Model:    "test/geo-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if err == nil {
		t.Fatal("exhausted geo pool must error")
	}
	var perr *models.ProviderError
	if !errors.As(err, &perr) || perr.Type != models.ErrorTypeGeo {
		t.Fatalf("exhaustion must stay a geo provider error: %v", err)
	}
	if !strings.Contains(perr.Message, "all 1 pooled proxies") {
		t.Fatalf("exhaustion message must report the pool size: %q", perr.Message)
	}
}

func TestDoWithProxyRotation_BadTransportNeverGoesDirect(t *testing.T) {
	// Unbuildable proxy URL: rotation has nothing to advance to, so the
	// build error surfaces. No network access happens either way.
	ctx := &execContext{
		proxyResolver: func(_ *PluginRecord, _ map[string]any) (string, string, error) {
			return "px-bogus", "bogus-scheme://example.com:8080", nil
		},
		triedProxies: map[string]bool{},
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
		proxyResolver: func(_ *PluginRecord, _ map[string]any) (string, string, error) {
			calls++
			return "px-1", "http://10.9.9.9:8080", nil
		},
	}
	if err := ctx.beginRequest(); err != nil {
		t.Fatal(err)
	}
	if ctx.proxyURL == "" {
		t.Fatal("route not resolved")
	}
	// Same single proxy: second rotation attempt stops, no infinite loop.
	if ctx.rotateProxy() {
		t.Fatal("rotation must stop when the resolver repeats a tried proxy")
	}
	if calls != 2 {
		t.Fatalf("expected 2 resolutions, got %d", calls)
	}
}
