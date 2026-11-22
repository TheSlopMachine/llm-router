package virtual

import (
	"context"
	"testing"
)

func TestProviderVirtualModelsSwitchRestoresEnabledState(t *testing.T) {
	service, _ := newSyncTestStack(t)
	ctx := context.Background()

	groups, err := service.SyncProviderModels(ctx, "demo")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	chat := groupByEndpoint(groups)["chat"].Virtual
	if chat == nil || !chat.Disabled {
		t.Fatalf("new managed models start disabled: %+v", chat)
	}
	enabledChat := *chat
	enabledChat.Disabled = false
	if err := service.Update(ctx, chat.ID, &enabledChat); err != nil {
		t.Fatalf("enable chat: %v", err)
	}

	settings, err := service.ProviderSettings(ctx, "demo")
	if err != nil || !settings.Enabled {
		t.Fatalf("default settings: %+v, %v; want enabled", settings, err)
	}

	if _, err := service.SetProviderVirtualModelsEnabled(ctx, "demo", false); err != nil {
		t.Fatalf("switch off: %v", err)
	}
	if all, err := service.List(ctx); err != nil || len(all) != 0 {
		t.Fatalf("switched off lists %d managed models (err %v), want 0", len(all), err)
	}
	if settings, err := service.ProviderSettings(ctx, "demo"); err != nil || settings.Enabled {
		t.Fatalf("settings after off: %+v, %v; want disabled", settings, err)
	}
	if _, err := service.SyncProviderModels(ctx, "demo"); err != nil {
		t.Fatalf("sync while off: %v", err)
	}
	if all, _ := service.List(ctx); len(all) != 0 {
		t.Fatalf("sync while off created %d models, want 0", len(all))
	}
	// switching off twice keeps the stored state
	if _, err := service.SetProviderVirtualModelsEnabled(ctx, "demo", false); err != nil {
		t.Fatalf("switch off again: %v", err)
	}

	groups, err = service.SetProviderVirtualModelsEnabled(ctx, "demo", true)
	if err != nil {
		t.Fatalf("switch on: %v", err)
	}
	bySlug := groupByEndpoint(groups)
	if restored := bySlug["chat"].Virtual; restored == nil || restored.Disabled {
		t.Fatalf("chat restored enabled: %+v", restored)
	}
	if restored := bySlug["speech"].Virtual; restored == nil || !restored.Disabled {
		t.Fatalf("speech restored disabled: %+v", restored)
	}
	if settings, err := service.ProviderSettings(ctx, "demo"); err != nil || !settings.Enabled {
		t.Fatalf("settings after on: %+v, %v; want enabled", settings, err)
	}
}

func TestProviderVirtualSettingsUnknownProvider(t *testing.T) {
	service, _ := newSyncTestStack(t)
	ctx := context.Background()
	if _, err := service.ProviderSettings(ctx, "missing"); err == nil {
		t.Fatal("settings of a missing provider succeeded, want an error")
	}
	if _, err := service.SetProviderVirtualModelsEnabled(ctx, "missing", false); err == nil {
		t.Fatal("switch of a missing provider succeeded, want an error")
	}
}
