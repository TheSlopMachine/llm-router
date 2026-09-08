package provider_test

import (
	"encoding/json"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func containsProvider(providers []*models.ProviderInstance, id string) bool {
	for _, p := range providers {
		if p.ID == id {
			return true
		}
	}
	return false
}

func TestProviderService_CreateAndGet(t *testing.T) {
	database := testutil.SetupTestDB(t)
	svc := provider.NewService(database)

	inst, err := svc.Create(provider.CreateOptions{
		Name: "My LLM", TypeKey: "custom",
		Config: map[string]any{"base_url": "https://api.example.com/v1/"},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if inst.ID != "custom:my-llm" {
		t.Fatalf("id: got %q, want %q", inst.ID, "custom:my-llm")
	}
	if inst.BaseURL() != "https://api.example.com/v1" {
		t.Fatalf("base_url should be normalized (no trailing slash): got %q", inst.BaseURL())
	}

	p, err := svc.Get("custom:my-llm")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if p.TypeKey != "custom" {
		t.Fatalf("type: got %q, want %q", p.TypeKey, "custom")
	}
}

func TestProviderService_CreateLuaType(t *testing.T) {
	database := testutil.SetupTestDB(t)
	svc := provider.NewService(database)

	inst, err := svc.Create(provider.CreateOptions{Name: "Zen", TypeKey: "opencode-zen"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if inst.ID != "opencode-zen" {
		t.Fatalf("id: got %q, want %q", inst.ID, "opencode-zen")
	}

	qualified, err := svc.Create(provider.CreateOptions{Name: "Zen EU", TypeKey: "opencode-zen", Qualifier: "eu"})
	if err != nil {
		t.Fatalf("create qualified failed: %v", err)
	}
	if qualified.ID != "opencode-zen:eu" {
		t.Fatalf("id: got %q", qualified.ID)
	}

	got, err := svc.GetByTypeAndQualifier("opencode-zen", "eu")
	if err != nil || got.ID != qualified.ID {
		t.Fatalf("get by type+qualifier failed: %+v %v", got, err)
	}
}

func TestProviderService_Create_Validation(t *testing.T) {
	database := testutil.SetupTestDB(t)
	svc := provider.NewService(database)

	if _, err := svc.Create(provider.CreateOptions{Name: "", TypeKey: "custom"}); err == nil {
		t.Fatal("expected error for empty name")
	}
	if _, err := svc.Create(provider.CreateOptions{Name: "Test"}); err == nil {
		t.Fatal("expected error for empty type_key")
	}
	if _, err := svc.Create(provider.CreateOptions{Name: "Test", TypeKey: "custom"}); err == nil {
		t.Fatal("expected error for custom without base_url")
	}
	if _, err := svc.Create(provider.CreateOptions{
		Name: "Test", TypeKey: "custom", Config: map[string]any{"base_url": "not-a-url"},
	}); err == nil {
		t.Fatal("expected error for invalid base_url")
	}
}

func TestProviderService_UpdateDelete(t *testing.T) {
	database := testutil.SetupTestDB(t)
	svc := provider.NewService(database)

	cp, err := svc.CreateCustom("My LLM", "https://api.example.com/v1/", "https://example.com/icon.svg")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if cp.ID != "custom:my-llm" {
		t.Fatalf("id: got %q, want %q", cp.ID, "custom:my-llm")
	}

	updated, err := svc.Update(cp.ID, provider.UpdateOptions{
		Name: "My LLM Updated", Config: map[string]any{"base_url": "https://new.example.com/v1"},
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updated.Name != "My LLM Updated" {
		t.Fatalf("name: got %q", updated.Name)
	}

	providers, err := svc.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if !containsProvider(providers, "custom:my-llm") {
		t.Fatal("custom provider missing from list")
	}

	if err := svc.DeleteCustom("my-llm"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := svc.Get("custom:my-llm"); err == nil {
		t.Fatal("expected deleted provider lookup to fail")
	}
}

func TestProviderService_DeleteCascadesCredentials(t *testing.T) {
	database := testutil.SetupTestDB(t)
	svc := provider.NewService(database)
	svc.RegisterGoAdapter(testutil.NewMockAdapter("mock"))

	inst, err := svc.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	credSvc := credential.New(database, svc)
	cred, err := credSvc.Add(credential.AddOptions{
		ProviderID: inst.ID, Label: "test", Data: map[string]any{"api_key": "key"},
	})
	if err != nil {
		t.Fatalf("add credential: %v", err)
	}
	if err := svc.Delete(inst.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := credSvc.Get(cred.ID); err == nil {
		t.Fatal("credential should be cascaded on provider delete")
	}
}

func TestProviderService_EnsureSeeded(t *testing.T) {
	database := testutil.SetupTestDB(t)
	svc := provider.NewService(database)

	if err := svc.EnsureSeeded(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.Get("agents"); err != nil {
		t.Fatalf("agents provider missing after seed: %v", err)
	}
	// Idempotent.
	if err := svc.EnsureSeeded(); err != nil {
		t.Fatalf("second seed: %v", err)
	}
}

func TestProviderService_EnsureSeededCopiesPluginIcon(t *testing.T) {
	database := testutil.SetupTestDB(t)
	svc := provider.NewService(database)

	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	const iconSource = `--- @plugin Seeded Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.0.4
--- @allow_host example.com

llm_router.register("seeded-type", {
  icon = "https://example.com/seeded.svg",
  complete = function() end,
})
`
	if _, err := luaSvc.Install([]byte(iconSource), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	svc.SetLuaService(luaSvc)

	if err := svc.EnsureSeeded(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	inst, err := svc.Get("seeded-type")
	if err != nil {
		t.Fatalf("seeded provider missing: %v", err)
	}
	if inst.IconURL != "https://example.com/seeded.svg" {
		t.Fatalf("seeded icon: got %q", inst.IconURL)
	}
}

func TestProviderService_EnsureSeededBackfillsMissingIcon(t *testing.T) {
	database := testutil.SetupTestDB(t)
	svc := provider.NewService(database)

	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	const iconSource = `--- @plugin Backfill Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.0.4
--- @allow_host example.com

llm_router.register("backfill-type", {
  icon = "https://example.com/backfill.svg",
  complete = function() end,
})
`
	if _, err := luaSvc.Install([]byte(iconSource), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	svc.SetLuaService(luaSvc)

	// Pre-existing rows: one without icon (backfill), one with custom icon (keep).
	if _, err := svc.Create(provider.CreateOptions{Name: "Bare", TypeKey: "backfill-type"}); err != nil {
		t.Fatalf("create bare: %v", err)
	}
	custom, err := svc.Create(provider.CreateOptions{Name: "Custom", TypeKey: "backfill-type", Qualifier: "q"})
	if err != nil {
		t.Fatalf("create custom: %v", err)
	}
	if _, err := svc.Update(custom.ID, provider.UpdateOptions{Name: "Custom", IconURL: "https://example.com/mine.svg"}); err != nil {
		t.Fatalf("set custom icon: %v", err)
	}

	if err := svc.EnsureSeeded(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	bare, err := svc.Get("backfill-type")
	if err != nil {
		t.Fatalf("bare provider missing: %v", err)
	}
	if bare.IconURL != "https://example.com/backfill.svg" {
		t.Fatalf("backfilled icon: got %q", bare.IconURL)
	}
	kept, err := svc.Get(custom.ID)
	if err != nil {
		t.Fatalf("custom provider missing: %v", err)
	}
	if kept.IconURL != "https://example.com/mine.svg" {
		t.Fatalf("custom icon overwritten: got %q", kept.IconURL)
	}
}

func TestProviderService_MigratesLegacyCustom(t *testing.T) {
	database := testutil.SetupTestDB(t)
	type legacyCustomProvider struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		BaseURL   string    `json:"base_url"`
		IconURL   string    `json:"icon_url"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	legacy := legacyCustomProvider{ID: "old-one", Name: "Old One", BaseURL: "https://old.example.com/v1/"}
	if err := database.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketCustomProviders)
		raw, _ := json.Marshal(legacy)
		return b.Put([]byte(legacy.ID), raw)
	}); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}

	svc := provider.NewService(database)
	got, err := svc.Get("custom:old-one")
	if err != nil {
		t.Fatalf("migrated provider missing: %v", err)
	}
	if got.BaseURL() != "https://old.example.com/v1" {
		t.Fatalf("migrated base_url: %q", got.BaseURL())
	}
}

func TestProviderService_TypeKeysIncludesGoAdapters(t *testing.T) {
	database := testutil.SetupTestDB(t)
	svc := provider.NewService(database)
	svc.RegisterGoAdapter(testutil.NewMockAdapter("mock"))

	keys := svc.TypeKeys()
	found := false
	for _, k := range keys {
		if k == "mock" {
			found = true
		}
	}
	if !found {
		t.Fatalf("type keys missing mock: %v", keys)
	}
}

func TestProviderService_CustomSchemas(t *testing.T) {
	database := testutil.SetupTestDB(t)
	svc := provider.NewService(database)

	nodes, err := svc.ConfigSchema("custom")
	if err != nil || len(nodes) == 0 {
		t.Fatalf("custom config schema: %+v %v", nodes, err)
	}
	nodes, err = svc.CredentialSchema("custom")
	if err != nil || len(nodes) == 0 {
		t.Fatalf("custom credential schema: %+v %v", nodes, err)
	}
	nodes, err = svc.CredentialSchema("agents")
	if err != nil || len(nodes) == 0 {
		t.Fatalf("agents credential schema: %+v %v", nodes, err)
	}
}
