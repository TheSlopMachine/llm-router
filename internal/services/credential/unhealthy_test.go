package credential

import (
	"testing"
)

func TestDisableUnhealthyFirstWins(t *testing.T) {
	svc, _ := setupCredentialService(t)
	cred, err := svc.Add(AddOptions{ProviderID: "mock", Label: "c", Data: map[string]any{"api_key": "k"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := svc.DisableUnhealthy(cred.ID, "health check reported unhealthy"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err := svc.Get(cred.ID)
	if err != nil || !got.Disabled || got.DisabledBy != "healthcheck" || got.DisabledReason != "health check reported unhealthy" || got.DisabledAt == nil {
		t.Fatalf("unhealthy disable: %+v %v", got, err)
	}
	if err := svc.DisableUnhealthy(cred.ID, "second"); err != nil {
		t.Fatalf("second disable: %v", err)
	}
	got, _ = svc.Get(cred.ID)
	if got.DisabledReason != "health check reported unhealthy" {
		t.Fatalf("first reason wins, got %q", got.DisabledReason)
	}
}
