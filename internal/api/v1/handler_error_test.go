package v1

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestHandleRouterErrorSetsRetryAfter(t *testing.T) {
	future := time.Now().Add(90 * time.Second)
	rr := httptest.NewRecorder()
	(&Handler{}).handleRouterError(rr, &models.ProviderError{
		StatusCode: 429, Type: models.ErrorTypeRateLimit, Message: "slow", RetryAfter: &future,
	})
	got := rr.Header().Get("Retry-After")
	if got == "" {
		t.Fatal("rate limit must carry a Retry-After header")
	}
	if got != "91" && got != "90" {
		t.Fatalf("Retry-After: got %q want ~90", got)
	}
}

func TestHandleRouterErrorOmitsRetryAfter(t *testing.T) {
	for name, err := range map[string]error{
		"upstream":   &models.ProviderError{StatusCode: 502, Type: models.ErrorTypeUpstream, Message: "boom"},
		"overloaded": &models.ProviderError{StatusCode: 503, Type: models.ErrorTypeOverloaded, Message: "busy"},
	} {
		rr := httptest.NewRecorder()
		(&Handler{}).handleRouterError(rr, err)
		if got := rr.Header().Get("Retry-After"); got != "" {
			t.Errorf("%s: unexpected Retry-After %q", name, got)
		}
	}
	past := time.Now().Add(-time.Second)
	rr := httptest.NewRecorder()
	(&Handler{}).handleRouterError(rr, &models.ProviderError{
		StatusCode: 429, Type: models.ErrorTypeRateLimit, Message: "slow", RetryAfter: &past,
	})
	if got := rr.Header().Get("Retry-After"); got != "" {
		t.Errorf("past retry_after: unexpected Retry-After %q", got)
	}
}
