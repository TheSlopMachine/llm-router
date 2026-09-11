package modelinfo

import (
	"context"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestModelOverrides_EnableDisable(t *testing.T) {
	svc, _, _, _ := setupModelInfoService(t)

	if !svc.IsModelEnabled("modelinfo-test", "live-model") {
		t.Fatal("models are enabled by default")
	}
	if err := svc.SetOverride(models.ModelOverride{ProviderID: "modelinfo-test", Name: "live-model", Disabled: true}); err != nil {
		t.Fatalf("set override: %v", err)
	}
	if svc.IsModelEnabled("modelinfo-test", "live-model") {
		t.Fatal("override did not disable the model")
	}
	if err := svc.DeleteOverride("modelinfo-test", "live-model"); err != nil {
		t.Fatalf("delete override: %v", err)
	}
	if !svc.IsModelEnabled("modelinfo-test", "live-model") {
		t.Fatal("model stays disabled after override removal")
	}
}

func TestModelOverrides_MergedView(t *testing.T) {
	svc, _, _, _ := setupModelInfoService(t)
	ctx := context.Background()

	if err := svc.SetOverride(models.ModelOverride{ProviderID: "modelinfo-test", Name: "live-model", Disabled: true, Capabilities: []string{"tools"}}); err != nil {
		t.Fatalf("disable override: %v", err)
	}
	if err := svc.SetOverride(models.ModelOverride{ProviderID: "modelinfo-test", Name: "hand-added", Custom: true, DisplayName: "Hand Added", Capabilities: []string{"vision"}}); err != nil {
		t.Fatalf("custom override: %v", err)
	}

	views, err := svc.MergedView(ctx, "modelinfo-test")
	if err != nil {
		t.Fatalf("merged view: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("expected 2 models (live + custom), got %d", len(views))
	}
	var live, custom *ModelView
	for i := range views {
		switch views[i].Name {
		case "live-model":
			live = &views[i]
		case "hand-added":
			custom = &views[i]
		}
	}
	if live == nil || custom == nil {
		t.Fatalf("missing models in view: %v", views)
	}
	if !live.Disabled || live.Custom {
		t.Errorf("live-model flags: %+v", live)
	}
	if len(live.Capabilities) != 1 || live.Capabilities[0] != "tools" {
		t.Errorf("live-model capabilities: %v", live.Capabilities)
	}
	if !custom.Custom || custom.Disabled || custom.DisplayName != "Hand Added" {
		t.Errorf("hand-added flags: %+v", custom)
	}
}

func TestModelOverrides_Validation(t *testing.T) {
	svc, _, _, _ := setupModelInfoService(t)
	if err := svc.SetOverride(models.ModelOverride{Name: "x"}); err == nil {
		t.Error("expected error for missing provider id")
	}
	if err := svc.SetOverride(models.ModelOverride{ProviderID: "p"}); err == nil {
		t.Error("expected error for missing model name")
	}
}
