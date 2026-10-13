package shared

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestWaitReadyTimeoutExpiry pins the bound: a dead port errors without
// serving the full sleep cycle twice.
func TestWaitReadyTimeoutExpiry(t *testing.T) {
	start := time.Now()
	if err := WaitReadyTimeout("http://127.0.0.1:1", time.Millisecond); err == nil {
		t.Fatal("dead port succeeded, want backend-not-ready error")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("expiry took %v, want fast failure", elapsed)
	}
}

// TestWaitReadyTimeoutSuccess pins the ready path: a status-shaped 200
// answers nil.
func TestWaitReadyTimeoutSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"bootstrapped":true}`))
	}))
	defer server.Close()
	if err := WaitReadyTimeout(server.URL, 5*time.Second); err != nil {
		t.Fatalf("ready server: %v", err)
	}
}
