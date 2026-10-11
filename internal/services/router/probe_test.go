package router

import (
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestProbeResultTerminalPassthrough(t *testing.T) {
	result := probeResult(time.Now(), "", "", &models.ProviderError{
		StatusCode: 503,
		Code:       "model_unavailable",
		Message:    "missing api key",
	})
	if result.OK {
		t.Errorf("model_unavailable probe: got OK=true")
	}
	if result.Code != "model_unavailable" {
		t.Errorf("model_unavailable probe: got code=%q", result.Code)
	}
	if result.QuotaExceeded {
		t.Errorf("model_unavailable probe: QuotaExceeded must stay false so the dashboard disables the model")
	}
}

func TestProbeResultQuotaStaysTemporary(t *testing.T) {
	result := probeResult(time.Now(), "", "", &models.ProviderError{
		StatusCode: 429,
		Code:       "rate_limit",
		Message:    "slow down",
	})
	if result.Code != "rate_limit" {
		t.Errorf("rate_limit probe: got code=%q", result.Code)
	}
	if !result.QuotaExceeded {
		t.Errorf("rate_limit probe: QuotaExceeded must be true so the dashboard never disables the model")
	}
}
