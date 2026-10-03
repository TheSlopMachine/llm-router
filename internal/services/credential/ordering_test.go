package credential

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func addKey(t *testing.T, svc *Service, label string) *models.Credential {
	t.Helper()
	cred, err := svc.Add(AddOptions{
		ProviderID: "mock",
		Label:      label,
		Data:       map[string]any{"api_key": "key-" + label},
	})
	if err != nil {
		t.Fatalf("add %s: %v", label, err)
	}
	return cred
}

func TestCredentialService_All_ExcludesDisabled(t *testing.T) {
	svc, _ := setupCredentialService(t)
	a := addKey(t, svc, "a")
	addKey(t, svc, "b")

	disabled := true
	if err := svc.UpdateDetails(a.ID, nil, &disabled, nil); err != nil {
		t.Fatalf("disable: %v", err)
	}

	creds, err := svc.All("mock")
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(creds) != 1 || creds[0].Label != "b" {
		t.Fatalf("expected only enabled credential b, got %v", creds)
	}
}

func TestCredentialService_UpdateDetails_LabelAndData(t *testing.T) {
	svc, _ := setupCredentialService(t)
	cred := addKey(t, svc, "old")

	label := "new"
	if err := svc.UpdateDetails(cred.ID, &label, nil, map[string]any{"api_key": "rotated"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := svc.Get(cred.ID)
	if got.Label != "new" {
		t.Errorf("label: got %q, want %q", got.Label, "new")
	}
	if got.Data["api_key"] != "rotated" {
		t.Errorf("data not replaced: %v", got.Data)
	}

	if err := svc.UpdateDetails(cred.ID, nil, nil, map[string]any{}); err == nil {
		t.Error("expected validation error for empty replacement data")
	}
}

func TestCredentialService_All_StoredOrder(t *testing.T) {
	svc, _ := setupCredentialService(t)
	a := addKey(t, svc, "a")
	addKey(t, svc, "b")

	creds, err := svc.All("mock")
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(creds) != 2 {
		t.Fatalf("expected both credentials, got %v", creds)
	}

	// Disabled credentials stay stored but out of the routable set.
	disabled := true
	if err := svc.UpdateDetails(a.ID, nil, &disabled, nil); err != nil {
		t.Fatalf("disable: %v", err)
	}
	creds, _ = svc.All("mock")
	if len(creds) != 1 || creds[0].Label != "b" {
		t.Fatalf("pool after disable: %v", creds)
	}
}
