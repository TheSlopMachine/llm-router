package provider_test

import (
	"encoding/json"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func containsProvider(providers []*models.ProviderInstance, id string) bool {
	for _, provider := range providers {
		if provider.ID == id {
			return true
		}
	}
	return false
}

func TestProviderService_CreateAndGet(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)

	instance, err := service.Create(provider.CreateOptions{
		Name: "My LLM", TypeKey: "custom",
		Config: map[string]any{"base_url": "https://api.example.com/v1/"},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if instance.ID != "custom:my-llm" {
		t.Fatalf("id: got %q, want %q", instance.ID, "custom:my-llm")
	}
	if instance.BaseURL() != "https://api.example.com/v1" {
		t.Fatalf("base_url should be normalized (no trailing slash): got %q", instance.BaseURL())
	}

	provider, err := service.Get("custom:my-llm")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if provider.TypeKey != "custom" {
		t.Fatalf("type: got %q, want %q", provider.TypeKey, "custom")
	}
}

func TestProviderService_CreateLuaType(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)

	instance, err := service.Create(provider.CreateOptions{Name: "Zen", TypeKey: "opencode-zen"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if instance.ID != "opencode-zen" {
		t.Fatalf("id: got %q, want %q", instance.ID, "opencode-zen")
	}

	qualified, err := service.Create(provider.CreateOptions{Name: "Zen EU", TypeKey: "opencode-zen", Qualifier: "eu"})
	if err != nil {
		t.Fatalf("create qualified failed: %v", err)
	}
	if qualified.ID != "opencode-zen:eu" {
		t.Fatalf("id: got %q", qualified.ID)
	}

	got, err := service.GetByTypeAndQualifier("opencode-zen", "eu")
	if err != nil || got.ID != qualified.ID {
		t.Fatalf("get by type+qualifier failed: %+v %v", got, err)
	}
}

func TestProviderService_GetByTypeAndQualifierSlugifies(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)

	instance, err := service.Create(provider.CreateOptions{Name: "West", TypeKey: "q-type", Qualifier: "EU West"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if instance.ID != "q-type:eu-west" {
		t.Fatalf("id: got %q, want q-type:eu-west", instance.ID)
	}
	// The lookup must slugify exactly like uniqueID: the raw qualifier
	// "EU West" never appears in a stored ID.
	got, err := service.GetByTypeAndQualifier("q-type", "EU West")
	if err != nil || got.ID != instance.ID {
		t.Fatalf("slugified lookup failed: %+v %v", got, err)
	}
}

func TestProviderService_Create_Validation(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)

	if _, err := service.Create(provider.CreateOptions{Name: "", TypeKey: "custom"}); err == nil {
		t.Fatal("expected error for empty name")
	}
	if _, err := service.Create(provider.CreateOptions{Name: "Test"}); err == nil {
		t.Fatal("expected error for empty type_key")
	}
	if _, err := service.Create(provider.CreateOptions{Name: "Test", TypeKey: "custom"}); err == nil {
		t.Fatal("expected error for custom without base_url")
	}
	if _, err := service.Create(provider.CreateOptions{
		Name: "Test", TypeKey: "custom", Config: map[string]any{"base_url": "not-a-url"},
	}); err == nil {
		t.Fatal("expected error for invalid base_url")
	}
}

func TestProviderService_UpdateDelete(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)

	createdProvider, err := service.Create(provider.CreateOptions{Name: "My LLM", TypeKey: "custom",
		Config: map[string]any{"base_url": "https://api.example.com/v1/"}, IconURL: "https://example.com/icon.svg"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if createdProvider.ID != "custom:my-llm" {
		t.Fatalf("id: got %q, want %q", createdProvider.ID, "custom:my-llm")
	}

	updated, err := service.Update(createdProvider.ID, provider.UpdateOptions{
		Name: "My LLM Updated", Config: map[string]any{"base_url": "https://new.example.com/v1"},
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updated.Name != "My LLM Updated" {
		t.Fatalf("name: got %q", updated.Name)
	}

	providers, err := service.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if !containsProvider(providers, "custom:my-llm") {
		t.Fatal("custom provider missing from list")
	}

	if err := service.Delete("custom:my-llm"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := service.Get("custom:my-llm"); err == nil {
		t.Fatal("expected deleted provider lookup to fail")
	}
}

func TestProviderService_DeleteCascadesCredentials(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)
	service.RegisterGoAdapter(testutil.NewMockAdapter("mock"))

	instance, err := service.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	credentialService := credential.New(database, service)
	cred, err := credentialService.Add(credential.AddOptions{
		ProviderID: instance.ID, Label: "test", Data: map[string]any{"api_key": "key"},
	})
	if err != nil {
		t.Fatalf("add credential: %v", err)
	}
	if err := service.Delete(instance.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := credentialService.Get(cred.ID); err == nil {
		t.Fatal("credential should be cascaded on provider delete")
	}
}

func TestProviderService_EnsureSeeded(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)

	if err := service.EnsureSeeded(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := service.Get("virtual"); err != nil {
		t.Fatalf("virtual provider missing after seed: %v", err)
	}
	// Idempotent.
	if err := service.EnsureSeeded(); err != nil {
		t.Fatalf("second seed: %v", err)
	}
}

func TestProviderService_EnsureSeededCopiesPluginIcon(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)

	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	const iconSource = `--- @plugin Seeded Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("seeded-type", {
  icon = "https://example.com/seeded.svg",
  complete = function() end,
})
`
	if _, err := luaSvc.Install([]byte(iconSource), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	service.SetLuaService(luaSvc)

	if err := service.EnsureSeeded(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	instance, err := service.Get("seeded-type")
	if err != nil {
		t.Fatalf("seeded provider missing: %v", err)
	}
	if instance.IconURL != "https://example.com/seeded.svg" {
		t.Fatalf("seeded icon: got %q", instance.IconURL)
	}
}

func TestProviderService_SyncDefaultProvidersAfterRuntimeInstall(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)

	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	service.SetLuaService(luaSvc)

	if err := service.EnsureSeeded(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	const runtimeSource = `--- @plugin Runtime Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("runtime-type", {
  complete = function() end,
})
`
	if _, err := luaSvc.Install([]byte(runtimeSource), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	if err := service.SyncDefaultProviders(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	instance, err := service.Get("runtime-type")
	if err != nil {
		t.Fatalf("default provider missing after runtime install: %v", err)
	}
	if instance.Name != "Runtime Plugin" {
		t.Fatalf("name: got %q, want %q", instance.Name, "Runtime Plugin")
	}
	if !instance.IsUIReadonly {
		t.Fatalf("seeded singleton must be readonly: %+v", instance)
	}
}

func TestProviderService_EnsureSeededBackfillsMissingIcon(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)

	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	const iconSource = `--- @plugin Backfill Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("backfill-type", {
  icon = "https://example.com/backfill.svg",
  complete = function() end,
})
`
	if _, err := luaSvc.Install([]byte(iconSource), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	service.SetLuaService(luaSvc)

	// Pre-existing rows: one without icon (backfill), one with custom icon (keep).
	if _, err := service.Create(provider.CreateOptions{Name: "Bare", TypeKey: "backfill-type"}); err != nil {
		t.Fatalf("create bare: %v", err)
	}
	custom, err := service.Create(provider.CreateOptions{Name: "Custom", TypeKey: "backfill-type", Qualifier: "q"})
	if err != nil {
		t.Fatalf("create custom: %v", err)
	}
	if _, err := service.Update(custom.ID, provider.UpdateOptions{Name: "Custom", IconURL: "https://example.com/mine.svg"}); err != nil {
		t.Fatalf("set custom icon: %v", err)
	}

	if err := service.EnsureSeeded(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	bare, err := service.Get("backfill-type")
	if err != nil {
		t.Fatalf("bare provider missing: %v", err)
	}
	if bare.IconURL != "https://example.com/backfill.svg" {
		t.Fatalf("backfilled icon: got %q", bare.IconURL)
	}
	kept, err := service.Get(custom.ID)
	if err != nil {
		t.Fatalf("custom provider missing: %v", err)
	}
	if kept.IconURL != "https://example.com/mine.svg" {
		t.Fatalf("custom icon overwritten: got %q", kept.IconURL)
	}
}

func TestProviderService_LegacyCustomBucketStaysDropped(t *testing.T) {
	database := testutil.SetupTestDB(t)
	if err := database.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("custom_providers"))
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(map[string]any{"id": "old-one"})
		return b.Put([]byte("old-one"), raw)
	}); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}

	service := provider.NewService(database)
	if err := service.EnsureSeeded(); err != nil {
		t.Fatalf("seed after legacy: %v", err)
	}
	if _, err := service.Get("custom:old-one"); err == nil {
		t.Fatal("legacy custom rows must not resurrect")
	}
}

func TestProviderService_CustomSchemas(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)

	nodes, err := service.ConfigSchema("custom")
	if err != nil || len(nodes) == 0 {
		t.Fatalf("custom config schema: %+v %v", nodes, err)
	}
	nodes, err = service.CredentialSchema("custom")
	if err != nil || len(nodes) == 0 {
		t.Fatalf("custom credential schema: %+v %v", nodes, err)
	}
	nodes, err = service.CredentialSchema("virtual")
	if err != nil || len(nodes) == 0 {
		t.Fatalf("virtual credential schema: %+v %v", nodes, err)
	}
}

func TestProviderService_ManualCreateHasNoUIFlags(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)

	instance, err := service.Create(provider.CreateOptions{Name: "Zen", TypeKey: "opencode-zen"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if instance.IsUIReadonly || instance.IsUIHidden {
		t.Fatalf("manual row must not carry UI flags: %+v", instance)
	}
}

func TestProviderService_EnsureSeededMarksVirtualHiddenReadonly(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)

	if err := service.EnsureSeeded(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	virtualModel, err := service.Get("virtual")
	if err != nil {
		t.Fatalf("virtual provider missing: %v", err)
	}
	if !virtualModel.IsUIReadonly || !virtualModel.IsUIHidden {
		t.Fatalf("virtual row must be readonly+hidden: %+v", virtualModel)
	}
}

func TestProviderService_EnsureSeededMarksLuaSingletonsReadonly(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)

	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	const src = `--- @plugin Flag Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("flag-type", {
  complete = function() end,
})
`
	if _, err := luaSvc.Install([]byte(src), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	service.SetLuaService(luaSvc)

	if err := service.EnsureSeeded(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	instance, err := service.Get("flag-type")
	if err != nil {
		t.Fatalf("seeded provider missing: %v", err)
	}
	if !instance.IsUIReadonly {
		t.Fatalf("seeded singleton must be readonly: %+v", instance)
	}
	if instance.IsUIHidden {
		t.Fatalf("seeded singleton must not be hidden: %+v", instance)
	}
}

func TestProviderService_BackfillsSeedFlags(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)

	// Pre-flag rows: bare singleton ID, virtual row, and a qualifier row.
	if _, err := service.Create(provider.CreateOptions{Name: "Bare", TypeKey: "flag-type"}); err != nil {
		t.Fatalf("create bare: %v", err)
	}
	if _, err := service.Create(provider.CreateOptions{Name: "Virtual models", TypeKey: "virtual"}); err != nil {
		t.Fatalf("create virtual: %v", err)
	}
	qualified, err := service.Create(provider.CreateOptions{Name: "Q", TypeKey: "flag-type", Qualifier: "eu"})
	if err != nil {
		t.Fatalf("create qualified: %v", err)
	}

	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	const src = `--- @plugin Flag Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("flag-type", {
  complete = function() end,
})
`
	if _, err := luaSvc.Install([]byte(src), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	service.SetLuaService(luaSvc)

	if err := service.EnsureSeeded(); err != nil {
		t.Fatalf("seed: %v", err)
	}

	bare, err := service.Get("flag-type")
	if err != nil {
		t.Fatalf("bare row missing: %v", err)
	}
	if !bare.IsUIReadonly || bare.IsUIHidden {
		t.Fatalf("bare singleton must become readonly: %+v", bare)
	}

	virtualModel, err := service.Get("virtual")
	if err != nil {
		t.Fatalf("virtual row missing: %v", err)
	}
	if !virtualModel.IsUIReadonly || !virtualModel.IsUIHidden {
		t.Fatalf("virtual row must become readonly+hidden: %+v", virtualModel)
	}

	queried, err := service.Get(qualified.ID)
	if err != nil {
		t.Fatalf("qualified row missing: %v", err)
	}
	if queried.IsUIReadonly || queried.IsUIHidden {
		t.Fatalf("qualifier row must stay editable: %+v", queried)
	}
}

func TestProviderService_IsTypeAvailable(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)
	service.RegisterGoAdapter(testutil.NewMockAdapter("mock"))

	if !service.IsTypeAvailable("mock") {
		t.Error("Go adapter type must be available")
	}
	if service.IsTypeAvailable("missing-type") {
		t.Error("unknown type must be unavailable")
	}

	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	service.SetLuaService(luaSvc)
	const src = `--- @plugin Availability Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("avail-type", {
  complete = function() end,
})
`
	rec, err := luaSvc.Install([]byte(src), luaplugin.PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	if !service.IsTypeAvailable("avail-type") {
		t.Fatal("installed plugin type must be available")
	}

	if err := luaSvc.Delete(rec.ID); err != nil {
		t.Fatalf("delete plugin: %v", err)
	}
	if service.IsTypeAvailable("avail-type") {
		t.Error("deleted plugin type must be unavailable")
	}

	if _, err := luaSvc.Install([]byte(src), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("reinstall plugin: %v", err)
	}
	if !service.IsTypeAvailable("avail-type") {
		t.Error("reinstalled plugin type must be available again")
	}
}

func TestCreateReusesSingletonRow(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := provider.NewService(database)

	first, err := service.Create(provider.CreateOptions{Name: "Groq", TypeKey: "groq"})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := service.Create(provider.CreateOptions{Name: "Groq Again", TypeKey: "groq"})
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected singleton reuse, got %q then %q", first.ID, second.ID)
	}
	qualified, err := service.Create(provider.CreateOptions{Name: "Groq Work", TypeKey: "groq", Qualifier: "work"})
	if err != nil {
		t.Fatalf("qualified create: %v", err)
	}
	if qualified.ID == first.ID {
		t.Fatalf("qualified instance must not reuse the singleton row")
	}
}
