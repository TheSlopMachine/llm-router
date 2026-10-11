package provider

import (
	"context"
	"reflect"
	"testing"
	"time"

	stdErrors "errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

func setupGormProvider(t *testing.T) (*GormService, *db.Database, context.Context) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	return NewGorm(database), database, context.Background()
}

func seedProxyPool(t *testing.T, ctx context.Context, database *db.Database, identifier string) {
	t.Helper()
	now := util.Now()
	row := record.ProxyPool{ID: identifier, Name: identifier, CreatedAt: now, UpdatedAt: now}
	if err := gorm.G[record.ProxyPool](database.Handle, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(ctx, &row); err != nil {
		t.Fatalf("seed pool %q: %v", identifier, err)
	}
}

func TestSplitComposeRoundTrip(t *testing.T) {
	fixtures := []map[string]any{
		{},
		{"base_url": "https://api.example.com/v1"},
		{"proxy": map[string]any{"pool": "auto"}},
		{"disable_failed_credentials": true},
		{"disable_failed_credentials": false},
		{
			"base_url":                   "https://api.example.com/v1",
			"proxy":                      map[string]any{"pool": "custom-pool"},
			"disable_failed_credentials": true,
			"api_version":                "2024-01",
			"timeout_ms":                 float64(3000),
		},
		{"plugin_key": "value", "nested": map[string]any{"inner": "data"}},
	}
	for index, configuration := range fixtures {
		baseURL, proxyPoolID, disableFailed, settings := splitProviderConfig(configuration)
		composed := composeProviderConfig(baseURL, proxyPoolID, disableFailed, settings)
		secondBase, secondPool, secondDisable, secondSettings := splitProviderConfig(composed)
		recomposed := composeProviderConfig(secondBase, secondPool, secondDisable, secondSettings)
		if !reflect.DeepEqual(composed, recomposed) {
			t.Errorf("fixture %d: compose not idempotent: first %+v second %+v", index, composed, recomposed)
		}
		if _, ok := settings[providerConfigBaseURLKey]; ok {
			t.Errorf("fixture %d: settings holds reserved base_url", index)
		}
		if _, ok := settings[providerConfigProxyKey]; ok {
			t.Errorf("fixture %d: settings holds reserved proxy", index)
		}
		if _, ok := settings[providerConfigDisableFailedKey]; ok {
			t.Errorf("fixture %d: settings holds reserved disable key", index)
		}
	}
}

func TestComposeShape(t *testing.T) {
	base := "https://api.example.com/v1"
	pool := "auto"
	composed := composeProviderConfig(&base, &pool, true, map[string]any{"api_version": "v1"})
	if composed["base_url"] != base {
		t.Errorf("base_url: got %+v", composed["base_url"])
	}
	proxy, ok := composed["proxy"].(map[string]any)
	if !ok || proxy["pool"] != pool {
		t.Errorf("proxy.pool: got %+v", composed["proxy"])
	}
	if composed["disable_failed_credentials"] != true {
		t.Errorf("disable switch: got %+v", composed["disable_failed_credentials"])
	}
	if composed["api_version"] != "v1" {
		t.Errorf("plugin key: got %+v", composed["api_version"])
	}

	empty := composeProviderConfig(nil, nil, false, map[string]any{})
	if _, ok := empty["base_url"]; ok {
		t.Error("empty base_url must stay absent")
	}
	if _, ok := empty["proxy"]; ok {
		t.Error("empty proxy must stay absent")
	}
	if empty["disable_failed_credentials"] != false {
		t.Errorf("disable switch must read stored false, got %+v", empty["disable_failed_credentials"])
	}

	blankPool := "   "
	withBlank := composeProviderConfig(nil, &blankPool, false, map[string]any{})
	if _, ok := withBlank["proxy"]; ok {
		t.Error("blank pool must stay absent")
	}
}

func TestSplitReads(t *testing.T) {
	baseURL, proxyPoolID, disableFailed, settings := splitProviderConfig(map[string]any{
		"base_url":                   "https://api.example.com/v1",
		"proxy":                      map[string]any{"pool": " auto "},
		"disable_failed_credentials": true,
		"custom":                     "kept",
	})
	if baseURL == nil || *baseURL != "https://api.example.com/v1" {
		t.Errorf("base split: got %+v", baseURL)
	}
	if proxyPoolID == nil || *proxyPoolID != "auto" {
		t.Errorf("pool split trims: got %+v", proxyPoolID)
	}
	if !disableFailed {
		t.Error("disable split must read true")
	}
	if settings["custom"] != "kept" || len(settings) != 1 {
		t.Errorf("settings: got %+v", settings)
	}

	_, proxyMissing, disableMissing, _ := splitProviderConfig(map[string]any{"proxy": "not-a-map"})
	if proxyMissing != nil {
		t.Errorf("non-map proxy must read direct, got %+v", *proxyMissing)
	}
	if disableMissing {
		t.Error("absent switch must read false")
	}
	_, _, disableNonBool, _ := splitProviderConfig(map[string]any{"disable_failed_credentials": "yes"})
	if disableNonBool {
		t.Error("non-boolean switch must read false")
	}
}

func TestGormCreateGetRoundTrip(t *testing.T) {
	service, _, ctx := setupGormProvider(t)
	created, err := service.Create(ctx, CreateOptions{
		Name:    "My LLM",
		TypeKey: TypeCustom,
		Config:  map[string]any{"base_url": "https://api.example.com/v1/"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID != "custom:my-llm" {
		t.Fatalf("identifier: got %q want custom:my-llm", created.ID)
	}
	if created.BaseURL() != "https://api.example.com/v1" {
		t.Fatalf("base_url normalized: got %q", created.BaseURL())
	}
	if created.Config["disable_failed_credentials"] != false {
		t.Fatalf("disable switch stored: got %+v", created.Config["disable_failed_credentials"])
	}
	fetched, err := service.Get(ctx, "custom:my-llm")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.TypeKey != TypeCustom || fetched.Name != "My LLM" {
		t.Fatalf("fetched: %+v", fetched)
	}
	if fetched.BaseURL() != "https://api.example.com/v1" {
		t.Fatalf("fetched base_url: got %q", fetched.BaseURL())
	}
	if fetched.CreatedAt.IsZero() || fetched.UpdatedAt.IsZero() {
		t.Fatal("timestamps must stamp")
	}
}

func TestGormCreateValidation(t *testing.T) {
	service, _, ctx := setupGormProvider(t)
	if _, err := service.Create(ctx, CreateOptions{Name: "", TypeKey: TypeCustom}); err == nil {
		t.Error("empty name must fail")
	}
	if _, err := service.Create(ctx, CreateOptions{Name: "Test"}); err == nil {
		t.Error("empty type_key must fail")
	}
	if _, err := service.Create(ctx, CreateOptions{Name: "Test", TypeKey: "bad/type"}); err == nil {
		t.Error("slash type_key must fail")
	}
	if _, err := service.Create(ctx, CreateOptions{Name: "Test", TypeKey: "bad:type"}); err == nil {
		t.Error("colon type_key must fail")
	}
	if _, err := service.Create(ctx, CreateOptions{Name: "Test", TypeKey: TypeCustom}); err == nil {
		t.Error("custom without base_url must fail")
	}
	if _, err := service.Create(ctx, CreateOptions{Name: "Test", TypeKey: TypeCustom, Config: map[string]any{"base_url": "not-a-url"}}); err == nil {
		t.Error("invalid base_url must fail")
	}
	long := make([]byte, 101)
	for index := range long {
		long[index] = 'a'
	}
	if _, err := service.Create(ctx, CreateOptions{Name: string(long), TypeKey: TypeCustom, Config: map[string]any{"base_url": "https://api.example.com/v1"}}); err == nil {
		t.Error("long name must fail")
	}
}

func TestGormCreateSingletonReuse(t *testing.T) {
	service, _, ctx := setupGormProvider(t)
	first, err := service.Create(ctx, CreateOptions{Name: "Groq", TypeKey: "groq"})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := service.Create(ctx, CreateOptions{Name: "Groq Again", TypeKey: "groq"})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("singleton reuse: got %q then %q", first.ID, second.ID)
	}
	qualified, err := service.Create(ctx, CreateOptions{Name: "Groq Work", TypeKey: "groq", Qualifier: "work"})
	if err != nil {
		t.Fatalf("qualified: %v", err)
	}
	if qualified.ID == first.ID {
		t.Fatal("qualified row must not reuse singleton")
	}
}

func TestGormCreateQualifiedSlug(t *testing.T) {
	service, _, ctx := setupGormProvider(t)
	created, err := service.Create(ctx, CreateOptions{Name: "West", TypeKey: "q-type", Qualifier: "EU West"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID != "q-type:eu-west" {
		t.Fatalf("identifier: got %q want q-type:eu-west", created.ID)
	}
	fetched, err := service.GetByTypeAndQualifier(ctx, "q-type", "EU West")
	if err != nil || fetched.ID != created.ID {
		t.Fatalf("slugified lookup: %+v %v", fetched, err)
	}
}

func TestGormUniqueIDDedup(t *testing.T) {
	service, _, ctx := setupGormProvider(t)
	first, err := service.Create(ctx, CreateOptions{Name: "Same", TypeKey: TypeCustom, Config: map[string]any{"base_url": "https://api.example.com/v1"}})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := service.Create(ctx, CreateOptions{Name: "Same", TypeKey: TypeCustom, Config: map[string]any{"base_url": "https://api.example.com/v1"}})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.ID == first.ID || second.ID != "custom:same-2" {
		t.Fatalf("dedup: first %q second %q", first.ID, second.ID)
	}
}

func TestGormListGetByType(t *testing.T) {
	service, _, ctx := setupGormProvider(t)
	if _, err := service.Create(ctx, CreateOptions{Name: "B", TypeKey: TypeCustom, Config: map[string]any{"base_url": "https://b.example.com/v1"}}); err != nil {
		t.Fatalf("create b: %v", err)
	}
	if _, err := service.Create(ctx, CreateOptions{Name: "A", TypeKey: TypeCustom, Config: map[string]any{"base_url": "https://a.example.com/v1"}}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if _, err := service.Create(ctx, CreateOptions{Name: "Zen", TypeKey: "zen-type"}); err != nil {
		t.Fatalf("create zen: %v", err)
	}
	listed, err := service.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 3 || listed[0].ID > listed[1].ID || listed[1].ID > listed[2].ID {
		t.Fatalf("list sorted by id: %+v", listed)
	}
	byType, err := service.GetByType(ctx, "zen-type")
	if err != nil {
		t.Fatalf("by type: %v", err)
	}
	if len(byType) != 1 || byType[0].ID != "zen-type" {
		t.Fatalf("by type rows: %+v", byType)
	}
}

func TestGormUpdate(t *testing.T) {
	service, _, ctx := setupGormProvider(t)
	created, err := service.Create(ctx, CreateOptions{Name: "My LLM", TypeKey: TypeCustom, Config: map[string]any{"base_url": "https://api.example.com/v1/"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	disabled := true
	updated, err := service.Update(ctx, created.ID, UpdateOptions{
		Name:     "My LLM Updated",
		Config:   map[string]any{"base_url": "https://new.example.com/v1/", "disable_failed_credentials": true},
		IconURL:  "https://example.com/icon.svg",
		Disabled: &disabled,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "My LLM Updated" || updated.BaseURL() != "https://new.example.com/v1" {
		t.Fatalf("updated fields: %+v", updated)
	}
	if !updated.Disabled || updated.DisabledBy != models.DisabledByAdmin || updated.DisabledAt == nil {
		t.Fatalf("disabled state: %+v", updated)
	}
	if !models.CredentialAutomationOn(updated.Config) {
		t.Fatal("automation switch must read on")
	}
	enabled := false
	reenabled, err := service.Update(ctx, created.ID, UpdateOptions{Name: "My LLM Updated", Disabled: &enabled})
	if err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if reenabled.Disabled || reenabled.DisabledBy != "" || reenabled.DisabledReason != "" || reenabled.DisabledAt != nil {
		t.Fatalf("re-enable must clear: %+v", reenabled)
	}
}

func TestGormUpdateMissReturnsNotFound(t *testing.T) {
	service, _, ctx := setupGormProvider(t)
	if _, err := service.Update(ctx, "missing", UpdateOptions{Name: "Name"}); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("update miss: got %v want not found", err)
	}
	if _, err := service.Get(ctx, "missing"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("get miss: got %v want not found", err)
	}
}

func TestGormSystemDisableFirstWins(t *testing.T) {
	service, _, ctx := setupGormProvider(t)
	created, err := service.Create(ctx, CreateOptions{Name: "P", TypeKey: TypeCustom, Config: map[string]any{"base_url": "https://api.example.com/v1"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := service.SystemDisable(ctx, created.ID, "structural: DNS NXDOMAIN"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	fetched, err := service.Get(ctx, created.ID)
	if err != nil || !fetched.Disabled || fetched.DisabledBy != "system" || fetched.DisabledReason != "structural: DNS NXDOMAIN" || fetched.DisabledAt == nil {
		t.Fatalf("disabled: %+v %v", fetched, err)
	}
	if err := service.SystemDisable(ctx, created.ID, "structural: second"); err != nil {
		t.Fatalf("second: %v", err)
	}
	fetched, _ = service.Get(ctx, created.ID)
	if fetched.DisabledReason != "structural: DNS NXDOMAIN" {
		t.Fatalf("first reason wins: got %q", fetched.DisabledReason)
	}
}

func TestGormDeleteCascadesCredentials(t *testing.T) {
	service, database, ctx := setupGormProvider(t)
	created, err := service.Create(ctx, CreateOptions{Name: "Mock", TypeKey: "mock"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	now := util.Now()
	credentialRow := record.Credential{ID: "credential-one", ProviderID: created.ID, Label: "test", Data: map[string]any{"api_key": "key"}, CreatedAt: now, UpdatedAt: now}
	if err := gorm.G[record.Credential](database.Handle, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(ctx, &credentialRow); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	parkRow := record.CredentialPark{CredentialID: "credential-one", Reason: "rate", Until: now.Add(time.Hour), ParkedAt: now}
	if err := gorm.G[record.CredentialPark](database.Handle, clause.OnConflict{Columns: []clause.Column{{Name: "credential_id"}}, UpdateAll: true}).Create(ctx, &parkRow); err != nil {
		t.Fatalf("seed park: %v", err)
	}
	healthRow := record.CredentialHealth{CredentialID: "credential-one", LastCheckAt: now, LastStatus: "healthy"}
	if err := gorm.G[record.CredentialHealth](database.Handle, clause.OnConflict{Columns: []clause.Column{{Name: "credential_id"}}, UpdateAll: true}).Create(ctx, &healthRow); err != nil {
		t.Fatalf("seed health: %v", err)
	}
	if err := service.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := gorm.G[record.Credential](database.Handle, clause.Eq{Column: "id", Value: "credential-one"}).First(ctx); !stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
		t.Fatalf("credential must cascade, got %v", err)
	}
	if _, err := gorm.G[record.CredentialPark](database.Handle, clause.Eq{Column: "credential_id", Value: "credential-one"}).First(ctx); !stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
		t.Fatalf("park must cascade, got %v", err)
	}
	if _, err := service.Get(ctx, created.ID); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("provider get after delete: got %v", err)
	}
}

func TestGormDeleteMissReturnsNotFound(t *testing.T) {
	service, _, ctx := setupGormProvider(t)
	if err := service.Delete(ctx, "missing"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("delete miss: got %v want not found", err)
	}
}

func TestGormProxyPoolForeignKey(t *testing.T) {
	service, database, ctx := setupGormProvider(t)
	seedProxyPool(t, ctx, database, "pool-one")
	created, err := service.Create(ctx, CreateOptions{
		Name:    "Pooled",
		TypeKey: "pooled-type",
		Config:  map[string]any{"proxy": map[string]any{"pool": "pool-one"}},
	})
	if err != nil {
		t.Fatalf("create with pool: %v", err)
	}
	fetched, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	reference, err := models.ParseProxyPoolRef(fetched.Config)
	if err != nil || reference.Pool != "pool-one" {
		t.Fatalf("proxy ref: %+v %v", reference, err)
	}
	if _, err := service.Create(ctx, CreateOptions{
		Name:    "Dangling",
		TypeKey: "dangling-type",
		Config:  map[string]any{"proxy": map[string]any{"pool": "missing-pool"}},
	}); !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Fatalf("dangling pool: got %v want invalid input", err)
	}
	if _, err := gorm.G[record.ProxyPool](database.Handle, clause.Eq{Column: "id", Value: "pool-one"}).Delete(ctx); !stdErrors.Is(db.Translate(err), apierrors.ErrInvalidInput) {
		t.Fatalf("referenced pool delete: got %v want invalid input", err)
	}
	if err := service.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	if _, err := gorm.G[record.ProxyPool](database.Handle, clause.Eq{Column: "id", Value: "pool-one"}).Delete(ctx); err != nil {
		t.Fatalf("unreferenced pool delete: %v", err)
	}
}

func TestGormQualifierDerivesFromIdentifier(t *testing.T) {
	service, _, ctx := setupGormProvider(t)
	custom, err := service.Create(ctx, CreateOptions{Name: "C", TypeKey: TypeCustom, Config: map[string]any{"base_url": "https://api.example.com/v1"}})
	if err != nil {
		t.Fatalf("create custom: %v", err)
	}
	if custom.Qualifier != "" {
		t.Fatalf("custom qualifier: got %q want empty", custom.Qualifier)
	}
	singleton, err := service.Create(ctx, CreateOptions{Name: "Single", TypeKey: "single-type"})
	if err != nil {
		t.Fatalf("create singleton: %v", err)
	}
	if singleton.Qualifier != "" {
		t.Fatalf("singleton qualifier: got %q want empty", singleton.Qualifier)
	}
	qualified, err := service.Create(ctx, CreateOptions{Name: "Q", TypeKey: "single-type", Qualifier: "eu"})
	if err != nil {
		t.Fatalf("create qualified: %v", err)
	}
	fetched, err := service.Get(ctx, qualified.ID)
	if err != nil {
		t.Fatalf("get qualified: %v", err)
	}
	if fetched.Qualifier != "eu" {
		t.Fatalf("qualified qualifier: got %q want eu", fetched.Qualifier)
	}
}

func TestGormProviderRecordRoundTrip(t *testing.T) {
	now := util.Now()
	disabledAt := now
	original := &models.ProviderInstance{
		ID:             "round-type:eu",
		Name:           "Round",
		TypeKey:        "round-type",
		Qualifier:      "eu",
		Config:         map[string]any{"base_url": "https://api.example.com/v1", "proxy": map[string]any{"pool": "auto"}, "disable_failed_credentials": true, "plugin_key": "kept"},
		IconURL:        "https://example.com/icon.svg",
		IsUIReadonly:   true,
		Disabled:       true,
		DisabledBy:     models.DisabledBySystem,
		DisabledReason: "reason",
		DisabledAt:     &disabledAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	row := providerRecordFromModel(original)
	if row.BaseURL == nil || *row.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("base column: %+v", row.BaseURL)
	}
	if row.ProxyPoolID == nil || *row.ProxyPoolID != "auto" {
		t.Fatalf("pool column: %+v", row.ProxyPoolID)
	}
	if !row.DisableFailedCredentials {
		t.Fatal("disable column must read true")
	}
	if _, ok := row.Settings["base_url"]; ok {
		t.Fatal("settings must not hold base_url")
	}
	rebuilt := providerModelFromRecord(row)
	if rebuilt.BaseURL() != "https://api.example.com/v1" {
		t.Fatalf("base roundtrip: got %q", rebuilt.BaseURL())
	}
	if !models.CredentialAutomationOn(rebuilt.Config) {
		t.Fatal("automation must read on")
	}
	reference, err := models.ParseProxyPoolRef(rebuilt.Config)
	if err != nil || reference.Pool != "auto" {
		t.Fatalf("proxy roundtrip: %+v %v", reference, err)
	}
	if rebuilt.Config["plugin_key"] != "kept" {
		t.Fatalf("plugin key roundtrip: %+v", rebuilt.Config)
	}
}

func TestGormMethodsFailClosedWithoutHandle(t *testing.T) {
	service := NewGorm(nil)
	ctx := context.Background()
	if _, err := service.Create(ctx, CreateOptions{Name: "N", TypeKey: "t"}); err == nil {
		t.Error("create without handle must fail")
	}
	if _, err := service.Get(ctx, "id"); err == nil {
		t.Error("get without handle must fail")
	}
	if _, err := service.List(ctx); err == nil {
		t.Error("list without handle must fail")
	}
	if err := service.Delete(ctx, "id"); err == nil {
		t.Error("delete without handle must fail")
	}
}

func TestGormEnsureSeededCreatesVirtualRow(t *testing.T) {
	service, _, ctx := setupGormProvider(t)
	if err := service.EnsureSeeded(ctx); err != nil {
		t.Fatalf("ensure seeded: %v", err)
	}
	row, err := service.Get(ctx, TypeVirtual)
	if err != nil {
		t.Fatalf("seeded virtual row must read back: %v", err)
	}
	if !row.IsUIReadonly || !row.IsUIHidden {
		t.Errorf("seeded virtual row must be readonly and hidden: %+v", row)
	}
	if err := service.SyncDefaultProviders(ctx); err != nil {
		t.Fatalf("sync default providers: %v", err)
	}
	listed, err := service.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 {
		t.Errorf("reseed must stay idempotent, got %d rows", len(listed))
	}
}
