package dashboard

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestHandleChatRouterErrorRendersTerminal(t *testing.T) {
	rr := httptest.NewRecorder()
	handleChatRouterError(rr, &models.ProviderError{
		StatusCode: 429, Code: "rate_limit", Message: "slow",
	}, &Handler{})
	if got := rr.Header().Get("Retry-After"); got != "" {
		t.Fatalf("terminal errors carry no Retry-After, got %q", got)
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
