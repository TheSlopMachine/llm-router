package v1

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestHandleRouterErrorRendersTerminal(t *testing.T) {
	rr := httptest.NewRecorder()
	(&Handler{}).handleRouterError(rr, &models.ProviderError{
		StatusCode: 429, Code: "rate_limit", Message: "slow",
	})
	if got := rr.Header().Get("Retry-After"); got != "" {
		t.Fatalf("unsupplied headers stay absent, got %q", got)
	}
	var body models.OpenAIError
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "rate_limit" || body.Error.Message != "slow" {
		t.Fatalf("body: %+v", body)
	}
	if rr.Code != 429 {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestHandleRouterErrorRendersSuppliedHeaders(t *testing.T) {
	rr := httptest.NewRecorder()
	(&Handler{}).handleRouterError(rr, &models.ProviderError{
		StatusCode: 429, Code: "rate_limit", Message: "slow",
		Headers: map[string]string{"Retry-After": "3", "X-Custom": "drop"},
	})
	if got := rr.Header().Get("Retry-After"); got != "3" {
		t.Fatalf("Retry-After = %q, want %q", got, "3")
	}
	if got := rr.Header().Get("X-Custom"); got != "" {
		t.Fatalf("non-allow-listed header leaked, got %q", got)
	}
}
