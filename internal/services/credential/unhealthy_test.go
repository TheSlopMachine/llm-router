package credential

import (
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
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

func TestDisableByPluginFirstWins(t *testing.T) {
	svc, _ := setupCredentialService(t)
	cred, err := svc.Add(AddOptions{ProviderID: "mock", Label: "c", Data: map[string]any{"api_key": "k"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := svc.DisableByPlugin(cred.ID, "api key rejected"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err := svc.Get(cred.ID)
	if err != nil || !got.Disabled || got.DisabledBy != "plugin" || got.DisabledReason != "api key rejected" || got.DisabledAt == nil {
		t.Fatalf("plugin disable: %+v %v", got, err)
	}
	if err := svc.DisableByPlugin(cred.ID, "second"); err != nil {
		t.Fatalf("second disable: %v", err)
	}
	got, _ = svc.Get(cred.ID)
	if got.DisabledReason != "api key rejected" {
		t.Fatalf("first reason wins, got %q", got.DisabledReason)
	}
	// Plugin writes never override admin disables.
	disabled := true
	if err := svc.UpdateDetails(cred.ID, nil, &disabled, nil); err != nil {
		t.Fatalf("admin disable: %v", err)
	}
	if err := svc.EnableByPlugin(cred.ID); err != nil {
		t.Fatalf("enable: %v", err)
	}
	got, _ = svc.Get(cred.ID)
	if !got.Disabled || got.DisabledBy != "admin" {
		t.Fatalf("admin cause must survive plugin enable: %+v", got)
	}
}

func TestEnableByPluginClearsOnlyPluginCause(t *testing.T) {
	svc, _ := setupCredentialService(t)
	cred, err := svc.Add(AddOptions{ProviderID: "mock", Label: "c", Data: map[string]any{"api_key": "k"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	// Enabling an enabled key is a no-op success.
	if err := svc.EnableByPlugin(cred.ID); err != nil {
		t.Fatalf("enable enabled: %v", err)
	}
	if err := svc.DisableByPlugin(cred.ID, "dead"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := svc.EnableByPlugin(cred.ID); err != nil {
		t.Fatalf("enable: %v", err)
	}
	got, _ := svc.Get(cred.ID)
	if got.Disabled || got.DisabledBy != "" || got.DisabledReason != "" || got.DisabledAt != nil {
		t.Fatalf("plugin cause must clear fully: %+v", got)
	}
	// Unknown IDs fail loudly.
	if err := svc.EnableByPlugin("missing"); err == nil {
		t.Fatal("enable of unknown id must fail")
	}
	if err := svc.DisableByPlugin("missing", "x"); err == nil {
		t.Fatal("disable of unknown id must fail")
	}
}

func TestParkRoundTrip(t *testing.T) {
	svc, _ := setupCredentialService(t)
	cred, err := svc.Add(AddOptions{ProviderID: "mock", Label: "c", Data: map[string]any{"api_key": "k"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if got, err := svc.Parked(cred.ID); err != nil || got != nil {
		t.Fatalf("unparked must read nil: %+v %v", got, err)
	}
	if err := svc.Park(cred.ID, time.Hour, "quota"); err != nil {
		t.Fatalf("park: %v", err)
	}
	got, err := svc.Parked(cred.ID)
	if err != nil || got == nil || got.Reason != "quota" || got.CredentialID != cred.ID {
		t.Fatalf("parked: %+v %v", got, err)
	}
	if !got.Until.After(time.Now()) || got.ParkedAt.IsZero() {
		t.Fatalf("park times: %+v", got)
	}
	if err := svc.Unpark(cred.ID); err != nil {
		t.Fatalf("unpark: %v", err)
	}
	if got, err := svc.Parked(cred.ID); err != nil || got != nil {
		t.Fatalf("unparked must read nil: %+v %v", got, err)
	}
	// Unparking a missing park succeeds; unknown credentials fail.
	if err := svc.Unpark(cred.ID); err != nil {
		t.Fatalf("second unpark: %v", err)
	}
	if err := svc.Park("missing", time.Hour, "x"); err == nil {
		t.Fatal("park of unknown id must fail")
	}
	if err := svc.Park(cred.ID, 0, "x"); err == nil {
		t.Fatal("non-positive ttl must fail")
	}
	if err := svc.Park(cred.ID, 100*24*time.Hour, "x"); err == nil {
		t.Fatal("overlong ttl must fail")
	}
}

func TestParkedExpiryDeletesOnRead(t *testing.T) {
	svc, _ := setupCredentialService(t)
	cred, err := svc.Add(AddOptions{ProviderID: "mock", Label: "c", Data: map[string]any{"api_key": "k"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	past := time.Now().Add(-time.Minute).UTC()
	if err := svc.parks.Put(cred.ID, &models.ParkEntry{
		CredentialID: cred.ID, Reason: "stale", Until: past, ParkedAt: past,
	}); err != nil {
		t.Fatalf("seed expired: %v", err)
	}
	if got, err := svc.Parked(cred.ID); err != nil || got != nil {
		t.Fatalf("lapsed park must read nil: %+v %v", got, err)
	}
	if _, err := svc.parks.Get(cred.ID); err == nil {
		t.Fatal("lapsed park must delete on read")
	}
}
