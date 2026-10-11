package credential

import (
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestDisableUnhealthyFirstWins(t *testing.T) {
	service, _ := setupCredentialService(t)
	cred, err := service.Add(AddOptions{ProviderID: "mock", Label: "c", Data: map[string]any{"api_key": "k"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := service.DisableUnhealthy(cred.ID, "health check reported unhealthy"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err := service.Get(cred.ID)
	if err != nil || !got.Disabled || got.DisabledBy != "healthcheck" || got.DisabledReason != "health check reported unhealthy" || got.DisabledAt == nil {
		t.Fatalf("unhealthy disable: %+v %v", got, err)
	}
	if err := service.DisableUnhealthy(cred.ID, "second"); err != nil {
		t.Fatalf("second disable: %v", err)
	}
	got, _ = service.Get(cred.ID)
	if got.DisabledReason != "health check reported unhealthy" {
		t.Fatalf("first reason wins, got %q", got.DisabledReason)
	}
}

func TestDisableByPluginFirstWins(t *testing.T) {
	service, _ := setupCredentialService(t)
	cred, err := service.Add(AddOptions{ProviderID: "mock", Label: "c", Data: map[string]any{"api_key": "k"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := service.DisableByPlugin(cred.ID, "api key rejected"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err := service.Get(cred.ID)
	if err != nil || !got.Disabled || got.DisabledBy != "plugin" || got.DisabledReason != "api key rejected" || got.DisabledAt == nil {
		t.Fatalf("plugin disable: %+v %v", got, err)
	}
	if err := service.DisableByPlugin(cred.ID, "second"); err != nil {
		t.Fatalf("second disable: %v", err)
	}
	got, _ = service.Get(cred.ID)
	if got.DisabledReason != "api key rejected" {
		t.Fatalf("first reason wins, got %q", got.DisabledReason)
	}
	// Plugin writes never override admin disables.
	disabled := true
	if err := service.UpdateDetails(cred.ID, nil, &disabled, nil); err != nil {
		t.Fatalf("admin disable: %v", err)
	}
	if err := service.EnableByPlugin(cred.ID); err != nil {
		t.Fatalf("enable: %v", err)
	}
	got, _ = service.Get(cred.ID)
	if !got.Disabled || got.DisabledBy != "admin" {
		t.Fatalf("admin cause must survive plugin enable: %+v", got)
	}
}

func TestEnableByPluginClearsOnlyPluginCause(t *testing.T) {
	service, _ := setupCredentialService(t)
	cred, err := service.Add(AddOptions{ProviderID: "mock", Label: "c", Data: map[string]any{"api_key": "k"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	// Enabling an enabled key is a no-op success.
	if err := service.EnableByPlugin(cred.ID); err != nil {
		t.Fatalf("enable enabled: %v", err)
	}
	if err := service.DisableByPlugin(cred.ID, "dead"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := service.EnableByPlugin(cred.ID); err != nil {
		t.Fatalf("enable: %v", err)
	}
	got, _ := service.Get(cred.ID)
	if got.Disabled || got.DisabledBy != "" || got.DisabledReason != "" || got.DisabledAt != nil {
		t.Fatalf("plugin cause must clear fully: %+v", got)
	}
	// Unknown IDs fail loudly.
	if err := service.EnableByPlugin("missing"); err == nil {
		t.Fatal("enable of unknown id must fail")
	}
	if err := service.DisableByPlugin("missing", "x"); err == nil {
		t.Fatal("disable of unknown id must fail")
	}
}

func TestParkRoundTrip(t *testing.T) {
	service, _ := setupCredentialService(t)
	cred, err := service.Add(AddOptions{ProviderID: "mock", Label: "c", Data: map[string]any{"api_key": "k"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if got, err := service.Parked(cred.ID); err != nil || got != nil {
		t.Fatalf("unparked must read nil: %+v %v", got, err)
	}
	if err := service.Park(cred.ID, time.Hour, "quota"); err != nil {
		t.Fatalf("park: %v", err)
	}
	got, err := service.Parked(cred.ID)
	if err != nil || got == nil || got.Reason != "quota" || got.CredentialID != cred.ID {
		t.Fatalf("parked: %+v %v", got, err)
	}
	if !got.Until.After(time.Now()) || got.ParkedAt.IsZero() {
		t.Fatalf("park times: %+v", got)
	}
	if err := service.Unpark(cred.ID); err != nil {
		t.Fatalf("unpark: %v", err)
	}
	if got, err := service.Parked(cred.ID); err != nil || got != nil {
		t.Fatalf("unparked must read nil: %+v %v", got, err)
	}
	// Unparking a missing park succeeds; unknown credentials fail.
	if err := service.Unpark(cred.ID); err != nil {
		t.Fatalf("second unpark: %v", err)
	}
	if err := service.Park("missing", time.Hour, "x"); err == nil {
		t.Fatal("park of unknown id must fail")
	}
	if err := service.Park(cred.ID, 0, "x"); err == nil {
		t.Fatal("non-positive ttl must fail")
	}
	if err := service.Park(cred.ID, 100*24*time.Hour, "x"); err == nil {
		t.Fatal("overlong ttl must fail")
	}
}

func TestParkedExpiryDeletesOnRead(t *testing.T) {
	service, _ := setupCredentialService(t)
	cred, err := service.Add(AddOptions{ProviderID: "mock", Label: "c", Data: map[string]any{"api_key": "k"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	past := time.Now().Add(-time.Minute).UTC()
	if err := service.parks.Put(cred.ID, &models.ParkEntry{
		CredentialID: cred.ID, Reason: "stale", Until: past, ParkedAt: past,
	}); err != nil {
		t.Fatalf("seed expired: %v", err)
	}
	if got, err := service.Parked(cred.ID); err != nil || got != nil {
		t.Fatalf("lapsed park must read nil: %+v %v", got, err)
	}
	if _, err := service.parks.Get(cred.ID); err == nil {
		t.Fatal("lapsed park must delete on read")
	}
}
