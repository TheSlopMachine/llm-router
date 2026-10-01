package proxypool

import (
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/testutil"
	proxypoollib "github.com/TheSlopMachine/proxypool"
)

func TestPenalizeProxy(t *testing.T) {
	service, err := New(testutil.SetupTestDB(t))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if service.PenalizeProxy("", "dns_resolution") {
		t.Fatal("empty URL must return false")
	}
	if service.PenalizeProxy("http://192.0.2.99:8080", "dns_resolution") {
		t.Fatal("unknown URL must return false")
	}
	proxyURL := "http://192.0.2.30:8080"
	service.cache.Set(proxypoollib.ProxyState{
		URL:           proxyURL,
		IP:            "192.0.2.30",
		Port:          8080,
		LastCheckedAt: time.Now(),
	})
	if !service.PenalizeProxy(proxyURL, "tls_certificate_verification") {
		t.Fatal("known URL must return true")
	}
	states := service.cache.All()
	if len(states) != 1 {
		t.Fatalf("cache holds %d states, want 1", len(states))
	}
	state := states[0]
	if !state.IsDead {
		t.Fatal("penalized proxy must be dead")
	}
	if state.ReviveAt.Before(time.Now()) {
		t.Fatal("ReviveAt must be in the future")
	}
	if state.Metadata["last_penalty_reason"] != "tls_certificate_verification" {
		t.Fatalf("reason must persist, got %q", state.Metadata["last_penalty_reason"])
	}
	if live, err := service.List(); err != nil || len(live) != 0 {
		t.Fatalf("penalized proxy must leave List, got %d %v", len(live), err)
	}
}
