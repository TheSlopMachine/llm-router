package proxypool

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestParseProxyMode(t *testing.T) {
	mode, ids := ParseProxyMode(nil)
	if mode != models.ProxyModeDisabled || len(ids) != 0 {
		t.Fatalf("nil config: %q %v", mode, ids)
	}
	mode, ids = ParseProxyMode(map[string]any{"proxy": map[string]any{
		"mode": "manual", "ids": []any{"px-a", "px-b", 42},
	}})
	if mode != models.ProxyModeManual || len(ids) != 2 || ids[0] != "px-a" {
		t.Fatalf("manual: %q %v", mode, ids)
	}
	mode, _ = ParseProxyMode(map[string]any{"proxy": map[string]any{"mode": "bogus"}})
	if mode != models.ProxyModeDisabled {
		t.Fatalf("unknown mode must fall back to disabled: %q", mode)
	}
}

func aliveProxy(t *testing.T, svc *Service, url, country string) *models.Proxy {
	t.Helper()
	p, err := svc.AddManual(url, country)
	if err != nil {
		t.Fatal(err)
	}
	p.Alive = true
	if err := svc.repo.Put(p.ID, p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveProxy_Matrix(t *testing.T) {
	svc := setup(t)
	us := aliveProxy(t, svc, "http://10.1.0.1:8080", "US")
	de := aliveProxy(t, svc, "http://10.1.0.2:8080", "DE")

	// Disabled goes direct.
	id, url, err := ResolveProxy(svc, models.ProxyModeDisabled, nil, "DE", false, "US", "groq")
	if err != nil || id != "" || url != "" {
		t.Fatalf("disabled: %q %q %v", id, url, err)
	}
	// Auto honors the location preference.
	id, _, err = ResolveProxy(svc, models.ProxyModeAuto, nil, "DE", false, "US", "groq")
	if err != nil || id != de.ID {
		t.Fatalf("auto: %q %v", id, err)
	}
	// Manual honors the explicit pick order.
	id, _, err = ResolveProxy(svc, models.ProxyModeManual, []string{us.ID, de.ID}, "DE", false, "US", "groq")
	if err != nil || id != us.ID {
		t.Fatalf("manual: %q %v", id, err)
	}
	// Manual with nothing usable fails loudly, never direct.
	if _, _, err = ResolveProxy(svc, models.ProxyModeManual, []string{"px-missing"}, "DE", false, "US", "groq"); err == nil {
		t.Fatal("manual without usable proxy must error")
	}
	// Forced location with no match settles for the best available exit:
	// the preference steers the order, it does not gate.
	id, _, err = ResolveProxy(svc, models.ProxyModeDisabled, nil, "JP", true, "US", "groq")
	if err != nil || id == "" {
		t.Fatalf("forced without match must fall through the pool: %q %v", id, err)
	}
	// Forced location with an empty pool fails loudly, never direct.
	emptyForce := setup(t)
	if _, _, err = ResolveProxy(emptyForce, models.ProxyModeDisabled, nil, "JP", true, "US", "groq"); err == nil {
		t.Fatal("forced with empty pool must error")
	}
	// Forced location with a match prefers it.
	id, _, err = ResolveProxy(svc, models.ProxyModeDisabled, nil, "DE", true, "US", "groq")
	if err != nil || id != de.ID {
		t.Fatalf("forced match: %q %v", id, err)
	}
	// Auto with an empty pool goes direct (best effort, no demand made).
	empty := setup(t)
	if id, url, err = ResolveProxy(empty, models.ProxyModeAuto, nil, "DE", false, "US", "groq"); err != nil || id != "" || url != "" {
		t.Fatalf("auto empty pool: %q %q %v", id, url, err)
	}
}
