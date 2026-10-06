package proxypool

import (
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/testutil"
	proxypoollib "github.com/TheSlopMachine/proxypool"
)

func TestMarkDead(t *testing.T) {
	service, err := New(testutil.SetupTestDB(t))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if service.MarkDead("", "dns_resolution") {
		t.Fatal("empty URL must return false")
	}
	if service.MarkDead("http://192.0.2.99:8080", "dns_resolution") {
		t.Fatal("unknown URL must return false")
	}

	t.Run("structural failures mark suspect", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			reason string
			fail   proxypoollib.FailReason
		}{
			{name: "early eof", reason: markDeadEarlyEOFReason, fail: proxypoollib.FailEOF},
			{name: "connection reset", reason: markDeadConnectionResetReason, fail: proxypoollib.FailEOF},
			{name: "connection refused", reason: markDeadConnectionRefusedReason, fail: proxypoollib.FailRefused},
		} {
			t.Run(tc.name, func(t *testing.T) {
				url := "http://192.0.2.40:8080"
				service.cache.Set(proxypoollib.ProxyState{URL: url, IP: "192.0.2.40", Port: 8080, Score: 8})
				if !service.MarkDead(url, tc.reason) {
					t.Fatal("known URL must become suspect")
				}
				state, ok := service.cache.Get(url)
				if !ok {
					t.Fatal("suspect proxy must remain cached")
				}
				if !state.Suspect || state.IsDead {
					t.Fatalf("state = suspect:%v dead:%v", state.Suspect, state.IsDead)
				}
				if state.FailReason != tc.fail {
					t.Fatalf("FailReason = %q, want %q", state.FailReason, tc.fail)
				}
			})
		}
	})

	t.Run("dns uses hard ban", func(t *testing.T) {
		proxyURL := "http://192.0.2.30:8080"
		service.cache.Set(proxypoollib.ProxyState{
			URL:           proxyURL,
			IP:            "192.0.2.30",
			Port:          8080,
			Score:         9.0,
			LastCheckedAt: time.Now(),
		})
		if !service.MarkDead(proxyURL, markDeadDNSResolutionReason) {
			t.Fatal("known URL must be marked dead")
		}
		state, ok := service.cache.Get(proxyURL)
		if !ok || !state.IsDead {
			t.Fatal("marked proxy must be dead")
		}
		if state.Metadata["manual_dead_reason"] != markDeadDNSResolutionReason {
			t.Fatalf("reason must persist, got %q", state.Metadata["manual_dead_reason"])
		}
		if live, err := service.List(); err != nil || len(live) != 0 {
			t.Fatalf("marked proxy must leave List, got %d %v", len(live), err)
		}
	})
}

func TestRecheckBannedValidation(t *testing.T) {
	service, err := New(testutil.SetupTestDB(t))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := service.RecheckBanned("not-a-reason", ""); err == nil || err != ErrInvalidBanReason {
		t.Fatalf("got err %v, want ErrInvalidBanReason", err)
	}
	if _, err := service.RecheckBanned("", "source"); err != nil {
		t.Fatalf("empty reason must be accepted, got %v", err)
	}
}
