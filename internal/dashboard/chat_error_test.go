package dashboard

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestHandleChatRouterErrorSetsRetryAfter(t *testing.T) {
	future := time.Now().Add(90 * time.Second)
	rr := httptest.NewRecorder()
	handleChatRouterError(rr, &models.ProviderError{
		StatusCode: 429, Type: models.ErrorTypeQuotaExceeded, Message: "quota", RetryAfter: &future,
	}, &Handler{})
	if got := rr.Header().Get("Retry-After"); got == "" {
		t.Fatal("quota error must carry a Retry-After header")
	}
	rr = httptest.NewRecorder()
	handleChatRouterError(rr, &models.ProviderError{
		StatusCode: 503, Type: models.ErrorTypeOverloaded, Message: "busy",
	}, &Handler{})
	if got := rr.Header().Get("Retry-After"); got != "" {
		t.Fatalf("overloaded: unexpected Retry-After %q", got)
	}
}
