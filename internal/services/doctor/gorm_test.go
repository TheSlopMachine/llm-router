package doctor

import (
	"context"
	"fmt"
	"testing"

	stdErrors "errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// stubGoBackends is a scripted GoBackendChecker: registered type keys
// resolve through mock adapters, everything else reads missing.
type stubGoBackends struct {
	adapters map[string]provider.GoAdapter
}

func (s *stubGoBackends) GoAdapterFor(typeKey string) (provider.GoAdapter, bool) {
	adapter, ok := s.adapters[typeKey]
	return adapter, ok
}

// stubPluginBackends is a scripted PluginBackendChecker: listed type keys
// resolve, everything else fails the lookup.
type stubPluginBackends struct {
	types map[string]bool
}

func (s *stubPluginBackends) Lookup(typeKey string) (*luaplugin.PluginRecord, error) {
	if s.types[typeKey] {
		return &luaplugin.PluginRecord{}, nil
	}
	return nil, fmt.Errorf("unknown type %q", typeKey)
}

// doctorGormStack wires a GormService over real GORM inventories with two
// seeded providers: demo-type (plugin-backed) and go-type (Go-backed).
// Both backends stub as installed, so the stack starts clean.
func doctorGormStack(t *testing.T) (*GormService, *db.Database, context.Context) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()
	seedGormDoctorProvider(t, ctx, database, "demo", "Demo", "demo-type")
	seedGormDoctorProvider(t, ctx, database, "go", "Go", "go-type")
	goBackends := &stubGoBackends{adapters: map[string]provider.GoAdapter{
		"go-type": testutil.NewMockAdapter("go-type"),
	}}
	pluginBackends := &stubPluginBackends{types: map[string]bool{"demo-type": true}}
	service := NewGorm(
		database,
		provider.NewGorm(database),
		virtual.NewGorm(database, nil, nil),
		token.NewGorm(database),
		goBackends,
		pluginBackends,
	)
	return service, database, ctx
}

func seedGormDoctorProvider(t *testing.T, ctx context.Context, database *db.Database, identifier, name, typeKey string) {
	t.Helper()
	now := util.Now()
	row := record.ProviderInstance{
		ID:        identifier,
		Name:      name,
		TypeKey:   typeKey,
		Settings:  map[string]any{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	rows := []record.ProviderInstance{row}
	if err := gorm.G[record.ProviderInstance](database.Handle, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
		t.Fatalf("seed provider %q: %v", identifier, err)
	}
}

func seedGormDoctorPlugin(t *testing.T, ctx context.Context, database *db.Database, pluginID string) {
	t.Helper()
	now := util.Now()
	rows := []record.Plugin{{
		ID:               pluginID,
		DisplayName:      "Doctor Plugin",
		Author:           "tester",
		Version:          "1.0.0",
		PluginAPIVersion: "1.0",
		Description:      "test",
		License:          "test",
		OriginPath:       "",
		Source:           []byte("source"),
		InstalledAt:      now,
		UpdatedAt:        now,
	}}
	if err := gorm.G[record.Plugin](database.Handle, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
		t.Fatalf("seed plugin %q: %v", pluginID, err)
	}
}

func seedGormDoctorStorage(t *testing.T, ctx context.Context, database *db.Database, pluginID, scope, key string, value []byte) {
	t.Helper()
	rows := []record.PluginStorage{{
		PluginID: pluginID,
		Scope:    scope,
		Key:      key,
		Value:    value,
	}}
	if err := gorm.G[record.PluginStorage](database.Handle, clause.OnConflict{
		Columns:   []clause.Column{{Name: "plugin_id"}, {Name: "scope"}, {Name: "key"}},
		UpdateAll: true,
	}).CreateInBatches(ctx, &rows, 500); err != nil {
		t.Fatalf("seed storage %q: %v", key, err)
	}
}

func hasGormCategory(report *InspectionReport, category IssueCategory) bool {
	for _, issue := range report.Issues {
		if issue.Category == category {
			return true
		}
	}
	return false
}

func gormCategoryKeys(report *InspectionReport, category IssueCategory) []string {
	for _, issue := range report.Issues {
		if issue.Category == category {
			return issue.Keys
		}
	}
	return nil
}

// assertNoOrphanCategories pins the foreign-key guarantee: the GORM
// report never emits the five retired orphan categories. The constants no
// longer exist, so the assertion runs on the raw category strings.
func assertNoOrphanCategories(t *testing.T, report *InspectionReport) {
	t.Helper()
	for _, category := range []IssueCategory{
		"orphan_credentials",
		"orphan_model_overrides",
		"orphan_model_infos",
		"orphan_virtual_models",
		"orphan_plugin_storage",
	} {
		if hasGormCategory(report, category) {
			t.Fatalf("retired category %q must never occur on the GORM path: %+v", category, report.Issues)
		}
	}
}

func TestGormInspect_CleanStack(t *testing.T) {
	service, _, ctx := doctorGormStack(t)
	report, err := service.Inspect(ctx)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if len(report.Issues) != 0 || report.TotalIssues != 0 {
		t.Fatalf("clean stack must report no issues: %+v", report.Issues)
	}
	assertNoOrphanCategories(t, report)
}

func TestGormInspect_RequiresInventories(t *testing.T) {
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()
	service := NewGorm(nil, nil, nil, nil, nil, nil)
	if _, err := service.Inspect(ctx); !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Fatalf("nil sqlite must fail invalid input, got %v", err)
	}
	_ = database
	service = NewGorm(database, nil, nil, nil, nil, nil)
	if _, err := service.Inspect(ctx); !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Fatalf("nil inventories must fail invalid input, got %v", err)
	}
}

func TestGormInspect_MissingBackendFlaggedAndDisabled(t *testing.T) {
	service, database, ctx := doctorGormStack(t)
	providerService := provider.NewGorm(database)
	ghost, err := providerService.Create(ctx, provider.CreateOptions{Name: "Ghost", TypeKey: "ghost-type"})
	if err != nil {
		t.Fatalf("create ghost provider: %v", err)
	}
	report, err := service.Inspect(ctx)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	keys := gormCategoryKeys(report, CategoryMissingProviderBackend)
	if len(keys) != 1 || keys[0] != ghost.ID {
		t.Fatalf("missing-backend keys = %v, want [%q]", keys, ghost.ID)
	}
	fixed, err := service.Fix(ctx, []IssueCategory{CategoryMissingProviderBackend})
	if err != nil {
		t.Fatalf("fix: %v", err)
	}
	if fixed != 1 {
		t.Fatalf("fixed %d providers, want 1", fixed)
	}
	stored, err := providerService.Get(ctx, ghost.ID)
	if err != nil || !stored.Disabled || stored.DisabledBy != "system" {
		t.Fatalf("ghost must disable by system: %+v %v", stored, err)
	}
	report, err = service.Inspect(ctx)
	if err != nil {
		t.Fatalf("re-inspect: %v", err)
	}
	if hasGormCategory(report, CategoryMissingProviderBackend) {
		t.Fatalf("disabled provider still flagged: %+v", report.Issues)
	}
}

func TestGormFix_MissingBackendDropsManagedVirtuals(t *testing.T) {
	service, database, ctx := doctorGormStack(t)
	providerService := provider.NewGorm(database)
	ghost, err := providerService.Create(ctx, provider.CreateOptions{Name: "Ghost", TypeKey: "ghost-type"})
	if err != nil {
		t.Fatalf("create ghost provider: %v", err)
	}
	now := util.Now()
	managedProvider := ghost.ID
	slug := "chat"
	virtualRows := []record.VirtualModel{{
		ID: "ghost-chat", Name: "Ghost Chat", NameKey: "ghost chat",
		Description: "managed", Instruction: "",
		ManagedProviderID: &managedProvider, ManagedEndpointSlug: &slug,
		CreatedAt: now, UpdatedAt: now,
	}}
	if err := gorm.G[record.VirtualModel](database.Handle, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &virtualRows, 500); err != nil {
		t.Fatalf("seed managed virtual: %v", err)
	}
	memberRows := []record.VirtualModelMember{{
		VirtualModelID: "ghost-chat", Position: 0, ProviderID: ghost.ID, ModelName: "ghost-model",
	}}
	if err := gorm.G[record.VirtualModelMember](database.Handle, clause.OnConflict{
		Columns: []clause.Column{{Name: "virtual_model_id"}, {Name: "position"}}, UpdateAll: true,
	}).CreateInBatches(ctx, &memberRows, 500); err != nil {
		t.Fatalf("seed virtual member: %v", err)
	}
	fixed, err := service.Fix(ctx, []IssueCategory{CategoryMissingProviderBackend})
	if err != nil {
		t.Fatalf("fix: %v", err)
	}
	if fixed != 2 {
		t.Fatalf("fixed %d, want 1 disable plus 1 virtual delete", fixed)
	}
	virtualCount, err := gorm.G[record.VirtualModel](database.Handle).Count(ctx, "*")
	if err != nil {
		t.Fatalf("count virtual models: %v", err)
	}
	if virtualCount != 0 {
		t.Fatalf("managed virtuals of disabled provider must delete, holds %d rows", virtualCount)
	}
	memberCount, err := gorm.G[record.VirtualModelMember](database.Handle).Count(ctx, "*")
	if err != nil {
		t.Fatalf("count virtual members: %v", err)
	}
	if memberCount != 0 {
		t.Fatalf("members of deleted virtuals must cascade, holds %d rows", memberCount)
	}
}

func TestGormInspect_MissingBackendSkipsDisabled(t *testing.T) {
	service, database, ctx := doctorGormStack(t)
	providerService := provider.NewGorm(database)
	parked, err := providerService.Create(ctx, provider.CreateOptions{Name: "Parked", TypeKey: "parked-type"})
	if err != nil {
		t.Fatalf("create parked provider: %v", err)
	}
	if err := providerService.SystemDisable(ctx, parked.ID, "test"); err != nil {
		t.Fatalf("disable parked: %v", err)
	}
	report, err := service.Inspect(ctx)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if hasGormCategory(report, CategoryMissingProviderBackend) {
		t.Fatalf("disabled provider without backend must pass: %+v", report.Issues)
	}
}

func TestGormInspect_DisabledBackendlessWithManagedVirtualsFlagged(t *testing.T) {
	service, database, ctx := doctorGormStack(t)
	providerService := provider.NewGorm(database)
	parked, err := providerService.Create(ctx, provider.CreateOptions{Name: "Parked", TypeKey: "parked-type"})
	if err != nil {
		t.Fatalf("create parked provider: %v", err)
	}
	if err := providerService.SystemDisable(ctx, parked.ID, "test"); err != nil {
		t.Fatalf("disable parked: %v", err)
	}
	now := util.Now()
	managedProvider := parked.ID
	slug := "chat"
	virtualRows := []record.VirtualModel{{
		ID: "parked-chat", Name: "Parked Chat", NameKey: "parked chat",
		Description: "managed", Instruction: "",
		ManagedProviderID: &managedProvider, ManagedEndpointSlug: &slug,
		CreatedAt: now, UpdatedAt: now,
	}}
	if err := gorm.G[record.VirtualModel](database.Handle, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &virtualRows, 500); err != nil {
		t.Fatalf("seed managed virtual: %v", err)
	}
	report, err := service.Inspect(ctx)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	keys := gormCategoryKeys(report, CategoryMissingProviderBackend)
	if len(keys) != 1 || keys[0] != parked.ID {
		t.Fatalf("missing-backend keys = %v, want [%q] while managed rows dangle", keys, parked.ID)
	}
	fixed, err := service.Fix(ctx, []IssueCategory{CategoryMissingProviderBackend})
	if err != nil {
		t.Fatalf("fix: %v", err)
	}
	if fixed != 2 {
		t.Fatalf("fixed %d, want 1 disable plus 1 virtual delete", fixed)
	}
	virtualCount, err := gorm.G[record.VirtualModel](database.Handle).Count(ctx, "*")
	if err != nil {
		t.Fatalf("count virtual models: %v", err)
	}
	if virtualCount != 0 {
		t.Fatalf("managed virtuals of disabled provider must delete, holds %d rows", virtualCount)
	}
}

func TestGormInspect_DanglingTokenModelFlaggedAndFixed(t *testing.T) {
	service, database, ctx := doctorGormStack(t)
	tokenService := token.NewGorm(database)
	created, err := tokenService.Create(ctx, token.CreateOptions{
		Name: "Dangling Token",
		Rules: models.TokenRules{
			AllowedModels: []models.ModelId{"demo/model-ok", "ghost-provider/model-gone", "not-a-model-id"},
		},
	})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	report, err := service.Inspect(ctx)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	keys := gormCategoryKeys(report, CategoryBrokenTokenIndexes)
	if len(keys) != 2 {
		t.Fatalf("dangling model keys = %v, want the ghost and unparseable entries", keys)
	}
	fixed, err := service.Fix(ctx, []IssueCategory{CategoryBrokenTokenIndexes})
	if err != nil {
		t.Fatalf("fix: %v", err)
	}
	if fixed != 2 {
		t.Fatalf("fixed %d entries, want 2", fixed)
	}
	stored, err := tokenService.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get token: %v", err)
	}
	if len(stored.Rules.AllowedModels) != 1 || string(stored.Rules.AllowedModels[0]) != "demo/model-ok" {
		t.Fatalf("live model must survive dangling fix: %+v", stored.Rules.AllowedModels)
	}
	report, err = service.Inspect(ctx)
	if err != nil {
		t.Fatalf("re-inspect: %v", err)
	}
	if hasGormCategory(report, CategoryBrokenTokenIndexes) {
		t.Fatalf("dangling models still flagged after fix: %+v", report.Issues)
	}
}

func TestGormInspect_CorruptStorageFlaggedAndSurvivesFix(t *testing.T) {
	service, database, ctx := doctorGormStack(t)
	seedGormDoctorPlugin(t, ctx, database, "doc-plugin")
	seedGormDoctorStorage(t, ctx, database, "doc-plugin", "flow", "device", []byte(`{"healthy":true}`))
	seedGormDoctorStorage(t, ctx, database, "doc-plugin", "broken", "row", []byte(`{invalid-json`))

	report, err := service.Inspect(ctx)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	keys := gormCategoryKeys(report, CategoryCorruptPluginStorage)
	if len(keys) != 1 {
		t.Fatalf("corrupt storage keys = %v, want exactly the broken row", keys)
	}
	fixed, err := service.Fix(ctx, nil)
	if err != nil {
		t.Fatalf("fix-all: %v", err)
	}
	if fixed != 0 {
		t.Fatalf("fix-all on corrupt-only stack must fix 0, got %d", fixed)
	}
	report, err = service.Inspect(ctx)
	if err != nil {
		t.Fatalf("re-inspect: %v", err)
	}
	if !hasGormCategory(report, CategoryCorruptPluginStorage) {
		t.Fatal("corrupt row must survive fix-all: no writer builds it, no one proves it orphaned")
	}
}

// TestGormInspect_DuplicatesPreventedByUnique pins the name_key UNIQUE
// index: two live virtual models with distinct names never flag
// duplicates, and true duplicates cannot land through the service.
func TestGormInspect_DuplicatesPreventedByUnique(t *testing.T) {
	service, database, ctx := doctorGormStack(t)
	virtualService := virtual.NewGorm(database, nil, nil)
	first := &models.VirtualModel{
		Name:   "Helper One",
		Models: []models.VirtualModelEntry{{ModelID: "demo/model-a"}},
	}
	if err := virtualService.Create(ctx, first); err != nil {
		t.Fatalf("create first: %v", err)
	}
	second := &models.VirtualModel{
		Name:   "Helper Two",
		Models: []models.VirtualModelEntry{{ModelID: "demo/model-b"}},
	}
	if err := virtualService.Create(ctx, second); err != nil {
		t.Fatalf("create second: %v", err)
	}
	report, err := service.Inspect(ctx)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if hasGormCategory(report, CategoryDuplicateVirtualModels) {
		t.Fatalf("distinct names must not flag duplicates: %+v", report.Issues)
	}
	clash := &models.VirtualModel{
		Name:   "HELPER one",
		Models: []models.VirtualModelEntry{{ModelID: "demo/model-c"}},
	}
	if err := virtualService.Create(ctx, clash); err == nil {
		t.Fatal("case-insensitive name clash must fail on the name_key index")
	}
}

// TestGormInspect_ProviderDeleteCascadesWithoutOrphans pins the
// foreign-key guarantee: deleting a provider cascades its credential,
// override, and snapshot rows, so the GORM report stays free of every
// retired orphan category.
func TestGormInspect_ProviderDeleteCascadesWithoutOrphans(t *testing.T) {
	service, database, ctx := doctorGormStack(t)
	seedGormDoctorProvider(t, ctx, database, "doomed", "Doomed", "doomed-type")
	now := util.Now()
	credentials := []record.Credential{{
		ID:         "doomed-cred",
		ProviderID: "doomed",
		Label:      "doomed",
		Data:       map[string]any{"api_key": "secret"},
		CreatedAt:  now,
		UpdatedAt:  now,
	}}
	if err := gorm.G[record.Credential](database.Handle).CreateInBatches(ctx, &credentials, 500); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	overrides := []record.ModelOverride{{
		ProviderID: "doomed",
		ModelName:  "model-x",
	}}
	if err := gorm.G[record.ModelOverride](database.Handle).CreateInBatches(ctx, &overrides, 500); err != nil {
		t.Fatalf("seed override: %v", err)
	}
	snapshots := []record.ModelInfoSnapshot{{
		ProviderID: "doomed",
		CachedAt:   now,
	}}
	if err := gorm.G[record.ModelInfoSnapshot](database.Handle).CreateInBatches(ctx, &snapshots, 500); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	providerService := provider.NewGorm(database)
	if err := providerService.Delete(ctx, "doomed"); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	if _, err := gorm.G[record.Credential](database.Handle, clause.Eq{Column: "id", Value: "doomed-cred"}).First(ctx); err == nil {
		t.Fatal("credential must cascade with its provider")
	}
	report, err := service.Inspect(ctx)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	assertNoOrphanCategories(t, report)
	if len(report.Issues) != 0 {
		t.Fatalf("cascaded delete must leave no issues: %+v", report.Issues)
	}
}

// TestGormFix_RetiredCategoriesFixNothing pins Fix behavior for the
// retired orphan categories: explicit selection fixes zero rows without
// an error because foreign keys leave nothing behind. The constants no
// longer exist, so the selection runs on the raw category strings.
func TestGormFix_RetiredCategoriesFixNothing(t *testing.T) {
	service, _, ctx := doctorGormStack(t)
	fixed, err := service.Fix(ctx, []IssueCategory{
		"orphan_credentials",
		"orphan_model_overrides",
		"orphan_model_infos",
		"orphan_virtual_models",
		"orphan_plugin_storage",
	})
	if err != nil {
		t.Fatalf("fix retired categories: %v", err)
	}
	if fixed != 0 {
		t.Fatalf("retired categories must fix 0, got %d", fixed)
	}
}

// TestGormInspect_IntegrityCheckPasses pins the allowlisted integrity
// probe on a healthy database: no integrity issue occurs.
func TestGormInspect_IntegrityCheckPasses(t *testing.T) {
	service, _, ctx := doctorGormStack(t)
	report, err := service.Inspect(ctx)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if hasGormCategory(report, CategoryIntegrityCheck) {
		t.Fatalf("healthy database must pass integrity: %+v", report.Issues)
	}
	if err := db.CheckIntegrity(ctx, service.sqlite.Handle); err != nil {
		t.Fatalf("direct integrity probe: %v", err)
	}
}

// TestGormFix_EmptySelectionFixesNothing guards the Fix contract: an
// unknown category selects no issue and fixes zero rows.
func TestGormFix_EmptySelectionFixesNothing(t *testing.T) {
	service, _, ctx := doctorGormStack(t)
	fixed, err := service.Fix(ctx, []IssueCategory{"no_such_category"})
	if err != nil {
		t.Fatalf("fix unknown category: %v", err)
	}
	if fixed != 0 {
		t.Fatalf("unknown category must fix 0, got %d", fixed)
	}
}
