package router

import (
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestProbeResultModelUnavailable(t *testing.T) {
	res := probeResult(time.Now(), "", "", &models.ProviderError{
		StatusCode: 503,
		Type:       models.ErrorTypeModelUnavailable,
		Message:    "missing api key",
	})
	if res.OK {
		t.Errorf("model_unavailable probe: got OK=true")
	}
	if res.Code != "model_unavailable" {
		t.Errorf("model_unavailable probe: got code=%q", res.Code)
	}
	if res.QuotaExceeded {
		t.Errorf("model_unavailable probe: QuotaExceeded must stay false so the dashboard disables the model")
	}
	if res.Summary != "model temporarily unavailable" {
		t.Errorf("model_unavailable probe: got summary=%q", res.Summary)
	}
}

func TestProbeResultQuotaStaysTemporary(t *testing.T) {
	res := probeResult(time.Now(), "", "", &models.ProviderError{
		StatusCode: 429,
		Type:       models.ErrorTypeRateLimit,
		Message:    "slow down",
	})
	if res.Code != "rate_limit" {
		t.Errorf("rate_limit probe: got code=%q", res.Code)
	}
	if !res.QuotaExceeded {
		t.Errorf("rate_limit probe: QuotaExceeded must be true so the dashboard never disables the model")
	}
}
