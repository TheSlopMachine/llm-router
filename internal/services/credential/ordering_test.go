package credential

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func addKey(t *testing.T, service *Service, label string) *models.Credential {
	t.Helper()
	cred, err := service.Add(AddOptions{
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
	service, _ := setupCredentialService(t)
	a := addKey(t, service, "a")
	addKey(t, service, "b")

	disabled := true
	if err := service.UpdateDetails(a.ID, nil, &disabled, nil); err != nil {
		t.Fatalf("disable: %v", err)
	}

	creds, err := service.All("mock")
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(creds) != 1 || creds[0].Label != "b" {
		t.Fatalf("expected only enabled credential b, got %v", creds)
	}
}

func TestCredentialService_UpdateDetails_LabelAndData(t *testing.T) {
	service, _ := setupCredentialService(t)
	cred := addKey(t, service, "old")

	label := "new"
	if err := service.UpdateDetails(cred.ID, &label, nil, map[string]any{"api_key": "rotated"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := service.Get(cred.ID)
	if got.Label != "new" {
		t.Errorf("label: got %q, want %q", got.Label, "new")
	}
	if got.Data["api_key"] != "rotated" {
		t.Errorf("data not replaced: %v", got.Data)
	}

	if err := service.UpdateDetails(cred.ID, nil, nil, map[string]any{}); err == nil {
		t.Error("expected validation error for empty replacement data")
	}
}

func TestCredentialService_All_StoredOrder(t *testing.T) {
	service, _ := setupCredentialService(t)
	a := addKey(t, service, "a")
	addKey(t, service, "b")

	creds, err := service.All("mock")
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(creds) != 2 {
		t.Fatalf("expected both credentials, got %v", creds)
	}

	// Disabled credentials stay stored but out of the routable set.
	disabled := true
	if err := service.UpdateDetails(a.ID, nil, &disabled, nil); err != nil {
		t.Fatalf("disable: %v", err)
	}
	creds, _ = service.All("mock")
	if len(creds) != 1 || creds[0].Label != "b" {
		t.Fatalf("pool after disable: %v", creds)
	}
}
