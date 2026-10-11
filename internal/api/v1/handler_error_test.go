package v1

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestHandleRouterErrorRendersTerminal(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Handler{}).handleRouterError(recorder, &models.ProviderError{
		StatusCode: 429, Code: "rate_limit", Message: "slow",
	})
	if got := recorder.Header().Get("Retry-After"); got != "" {
		t.Fatalf("unsupplied headers stay absent, got %q", got)
	}
	var body models.OpenAIError
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "rate_limit" || body.Error.Message != "slow" {
		t.Fatalf("body: %+v", body)
	}
	if recorder.Code != 429 {
		t.Fatalf("status: %d", recorder.Code)
	}
}

func TestHandleRouterErrorRendersSuppliedHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Handler{}).handleRouterError(recorder, &models.ProviderError{
		StatusCode: 429, Code: "rate_limit", Message: "slow",
		Headers: map[string]string{"Retry-After": "3", "X-Custom": "drop"},
	})
	if got := recorder.Header().Get("Retry-After"); got != "3" {
		t.Fatalf("Retry-After = %q, want %q", got, "3")
	}
	if got := recorder.Header().Get("X-Custom"); got != "" {
		t.Fatalf("non-allow-listed header leaked, got %q", got)
	}
}
