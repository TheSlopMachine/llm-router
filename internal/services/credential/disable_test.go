package credential

import (
	"testing"
)

func TestSystemDisableFirstWins(t *testing.T) {
	svc, _ := setupCredentialService(t)
	cred, err := svc.Add(AddOptions{ProviderID: "mock", Label: "k", Data: map[string]any{"api_key": "x"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := svc.SystemDisable(cred.ID, "auth: 401 invalid key"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err := svc.Get(cred.ID)
	if err != nil || !got.Disabled || got.DisabledBy != "system" || got.DisabledReason != "auth: 401 invalid key" || got.DisabledAt == nil {
		t.Fatalf("disabled state: %+v %v", got, err)
	}
	if err := svc.SystemDisable(cred.ID, "auth: second"); err != nil {
		t.Fatalf("second disable: %v", err)
	}
	got, _ = svc.Get(cred.ID)
	if got.DisabledReason != "auth: 401 invalid key" {
		t.Fatalf("first reason wins, got %q", got.DisabledReason)
	}
	if _, err := svc.All("mock"); err == nil {
		t.Fatal("disabled credential must leave the pool")
	}
	disabled := false
	if err := svc.UpdateDetails(cred.ID, nil, &disabled, nil); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	got, _ = svc.Get(cred.ID)
	if got.Disabled || got.DisabledBy != "" || got.DisabledReason != "" || got.DisabledAt != nil {
		t.Fatalf("re-enable must clear: %+v", got)
	}
}
