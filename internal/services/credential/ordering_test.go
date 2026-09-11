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

func TestCredentialService_Reorder(t *testing.T) {
	svc, _ := setupCredentialService(t)
	a := addKey(t, svc, "a")
	b := addKey(t, svc, "b")
	c := addKey(t, svc, "c")

	if err := svc.Reorder("mock", []string{c.ID, a.ID, b.ID}); err != nil {
		t.Fatalf("reorder: %v", err)
	}

	creds, err := svc.All("mock")
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	want := []string{c.ID, a.ID, b.ID}
	for i, id := range want {
		if creds[i].ID != id {
			t.Fatalf("position %d: got %s, want %s (order %v)", i, creds[i].ID, id, []int{creds[0].Order, creds[1].Order, creds[2].Order})
		}
	}

	// Disabled credentials stay in the reorder contract but out of the pool.
	disabled := true
	if err := svc.UpdateDetails(b.ID, nil, &disabled, nil); err != nil {
		t.Fatalf("disable: %v", err)
	}
	creds, _ = svc.All("mock")
	if len(creds) != 2 || creds[0].ID != c.ID || creds[1].ID != a.ID {
		t.Fatalf("pool after disable: %v", creds)
	}
}

func TestCredentialService_Reorder_Validation(t *testing.T) {
	svc, _ := setupCredentialService(t)
	a := addKey(t, svc, "a")

	if err := svc.Reorder("mock", []string{a.ID, a.ID}); err == nil {
		t.Error("expected error for duplicate id")
	}
	if err := svc.Reorder("mock", []string{"nonexistent"}); err == nil {
		t.Error("expected error for foreign id")
	}
	if err := svc.Reorder("mock", []string{}); err == nil {
		t.Error("expected error for incomplete list")
	}
}

func TestSortPool_ManualOrderBeatsUsage(t *testing.T) {
	creds := []*models.Credential{
		{ID: "unordered"},
		{ID: "second", Order: 2},
		{ID: "first", Order: 1},
	}
	SortPool(creds)
	if creds[0].ID != "first" || creds[1].ID != "second" || creds[2].ID != "unordered" {
		t.Fatalf("got %v", []string{creds[0].ID, creds[1].ID, creds[2].ID})
	}
}
