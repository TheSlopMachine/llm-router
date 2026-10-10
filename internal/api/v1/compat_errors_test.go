package v1

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestHandleCompatRouterErrorRendersSuppliedHeaders(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("anthropic-version", "2023-06-01")
	rr := httptest.NewRecorder()
	(&Handler{}).handleCompatRouterError(rr, req, &models.ProviderError{
		StatusCode: 429, Code: "rate_limit", Message: "slow",
		Headers: map[string]string{"Retry-After": "30"},
	})
	if got := rr.Header().Get("Retry-After"); got != "30" {
		t.Fatalf("Retry-After = %q, want %q", got, "30")
	}
	if rr.Code != 429 {
		t.Fatalf("status: %d", rr.Code)
	}
	var body struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Type != "error" || body.Error.Type != "rate_limit_error" {
		t.Fatalf("envelope: %+v", body)
	}
}
