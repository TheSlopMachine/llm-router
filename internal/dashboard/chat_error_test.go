package dashboard

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestHandleChatRouterErrorRendersTerminal(t *testing.T) {
	recorder := httptest.NewRecorder()
	handleChatRouterError(recorder, &models.ProviderError{
		StatusCode: 429, Code: "rate_limit", Message: "slow",
	}, &Handler{})
	if got := recorder.Header().Get("Retry-After"); got != "" {
		t.Fatalf("terminal errors carry no Retry-After, got %q", got)
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
