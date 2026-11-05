package dashboard

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginLimiterLocksAfterFreeFailuresAndDoubles(t *testing.T) {
	limiter := newLoginLimiter()
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }
	request := httptest.NewRequest("POST", "/api/llm-router/login", nil)
	request.RemoteAddr = "198.51.100.7:51234"
	key := loginLimiterKey(request, "admin")

	for i := 0; i < loginFreeFailures-1; i++ {
		limiter.recordFailure(key)
	}
	if wait := limiter.retryAfter(key); wait != 0 {
		t.Fatalf("free failures must not lock, got %v", wait)
	}
	limiter.recordFailure(key)
	if wait := limiter.retryAfter(key); wait != loginBaseLockout {
		t.Fatalf("first lockout = %v, want %v", wait, loginBaseLockout)
	}
	limiter.recordFailure(key)
	if wait := limiter.retryAfter(key); wait != 2*loginBaseLockout {
		t.Fatalf("second lockout = %v, want doubled", wait)
	}
	for i := 0; i < 20; i++ {
		limiter.recordFailure(key)
	}
	if wait := limiter.retryAfter(key); wait != loginMaxLockout {
		t.Fatalf("lockout must cap at %v, got %v", loginMaxLockout, wait)
	}

	otherPort := httptest.NewRequest("POST", "/api/llm-router/login", nil)
	otherPort.RemoteAddr = "198.51.100.7:60000"
	if loginLimiterKey(otherPort, "admin") != key {
		t.Fatal("the source port must not change the key")
	}

	limiter.recordSuccess(key)
	if wait := limiter.retryAfter(key); wait != 0 {
		t.Fatalf("success must clear the lockout, got %v", wait)
	}
}

func TestLoginLimiterIgnoresForwardedFor(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/llm-router/login", nil)
	request.RemoteAddr = "198.51.100.7:51234"
	request.Header.Set("X-Forwarded-For", "203.0.113.99")
	if key := loginLimiterKey(request, "admin"); key != "198.51.100.7\x00admin" {
		t.Fatalf("key must use the socket peer, got %q", key)
	}
}
