package provider_test

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func TestSystemDisableFirstWins(t *testing.T) {
	database := testutil.SetupTestDB(t)
	svc := provider.NewService(database)
	inst, err := svc.Create(provider.CreateOptions{
		Name: "P", TypeKey: "custom",
		Config: map[string]any{"base_url": "https://api.example.com/v1"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.SystemDisable(inst.ID, "structural: DNS NXDOMAIN"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err := svc.Get(inst.ID)
	if err != nil || !got.Disabled || got.DisabledBy != "system" || got.DisabledReason != "structural: DNS NXDOMAIN" || got.DisabledAt == nil {
		t.Fatalf("disabled state: %+v %v", got, err)
	}
	if err := svc.SystemDisable(inst.ID, "structural: second"); err != nil {
		t.Fatalf("second: %v", err)
	}
	got, _ = svc.Get(inst.ID)
	if got.DisabledReason != "structural: DNS NXDOMAIN" {
		t.Fatalf("first reason wins, got %q", got.DisabledReason)
	}
	off := false
	if _, err := svc.Update(inst.ID, provider.UpdateOptions{Name: "P", Disabled: &off}); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	got, _ = svc.Get(inst.ID)
	if got.Disabled || got.DisabledBy != "" || got.DisabledReason != "" || got.DisabledAt != nil {
		t.Fatalf("re-enable must clear: %+v", got)
	}
}
