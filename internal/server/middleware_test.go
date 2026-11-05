package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoopbackHostMiddlewareRejectsRebindedHosts(t *testing.T) {
	reached := false
	handler := loopbackHostMiddleware("127.0.0.1:38082", nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	for host, want := range map[string]int{
		"127.0.0.1:38082":    http.StatusOK,
		"localhost:38082":    http.StatusOK,
		"[::1]:38082":        http.StatusOK,
		"LOCALHOST:38082":    http.StatusOK,
		"evil.example:38082": http.StatusMisdirectedRequest,
		"127.0.0.1:38080":    http.StatusMisdirectedRequest,
		"127.0.0.1":          http.StatusMisdirectedRequest,
		"attacker.test:80":   http.StatusMisdirectedRequest,
		"":                   http.StatusMisdirectedRequest,
	} {
		reached = false
		request := httptest.NewRequest("GET", "/api/llm-router/dashboard/credentials", nil)
		request.Host = host
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != want {
			t.Errorf("host %q: status %d, want %d", host, recorder.Code, want)
		}
		if reached != (want == http.StatusOK) {
			t.Errorf("host %q: handler reached = %v", host, reached)
		}
	}
}

func TestSecurityHeadersMiddleware(t *testing.T) {
	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	for name, want := range map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "same-origin",
	} {
		if got := recorder.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}
