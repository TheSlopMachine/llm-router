package modelinfo

import (
	"context"
	"testing"

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

func setupGormModelInfo(t *testing.T) (*GormService, context.Context, *db.Database) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()
	seedGormProvider(t, database, ctx, "mock", "Mock")
	return NewGorm(database), ctx, database
}

func seedGormProvider(t *testing.T, database *db.Database, ctx context.Context, identifier, name string) {
	t.Helper()
	now := util.Now()
	row := record.ProviderInstance{
		ID:        identifier,
		Name:      name,
		TypeKey:   "mock",
		Settings:  map[string]any{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := gorm.G[record.ProviderInstance](database.Handle, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(ctx, &row); err != nil {
		t.Fatalf("seed sqlite provider %s: %v", identifier, err)
	}
}

func fullGormModel() models.ModelInfo {
	return models.ModelInfo{
		Name:                "alpha",
		DisplayName:         "Alpha",
		Description:         "first model",
		RPM:                 60,
		TPM:                 1000,
		RPD:                 100,
		ContextWindow:       4096,
		MaxTokens:           1024,
		Capabilities:        []string{"json_mode", "tools"},
		InputModalities:     []string{"image", "text"},
		OutputModalities:    []string{"text"},
		SupportedParameters: []string{"tools", "response_format"},
		Reasoning: &models.ModelReasoning{
			SupportedEfforts: []string{"low", "high"},
			DefaultEffort:    "low",
			DefaultEnabled:   true,
		},
		Endpoints: []string{"chat/completions", "embeddings"},
	}
}

func TestGormSetGetOverrideRoundTrip(t *testing.T) {
	service, ctx, _ := setupGormModelInfo(t)
	override := models.ModelOverride{
		ProviderID:       "mock",
		Name:             "live-model",
		Disabled:         true,
		Custom:           true,
		DisplayName:      "Live Model",
		Capabilities:     []string{"tools"},
		InputModalities:  []string{"text"},
		OutputModalities: []string{"text"},
	}
	if err := service.SetOverride(ctx, override); err != nil {
		t.Fatalf("set: %v", err)
	}
	fetched, err := service.GetOverride(ctx, "mock", "live-model")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.ProviderID != "mock" || fetched.Name != "live-model" {
		t.Fatalf("identity: %+v", fetched)
	}
	if !fetched.Disabled || !fetched.Custom || fetched.DisplayName != "Live Model" {
		t.Fatalf("flags: %+v", fetched)
	}
	if len(fetched.Capabilities) != 1 || fetched.Capabilities[0] != "tools" {
		t.Fatalf("capabilities: %+v", fetched.Capabilities)
	}
	if len(fetched.InputModalities) != 1 || fetched.InputModalities[0] != "text" {
		t.Fatalf("input modalities: %+v", fetched.InputModalities)
	}
	if len(fetched.OutputModalities) != 1 || fetched.OutputModalities[0] != "text" {
		t.Fatalf("output modalities: %+v", fetched.OutputModalities)
	}
	if _, err := service.GetOverride(ctx, "mock", "missing"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("get miss: got %v want not found", err)
	}
}

func TestGormSetOverrideValidation(t *testing.T) {
	service, ctx, _ := setupGormModelInfo(t)
	if err := service.SetOverride(ctx, models.ModelOverride{Name: "x"}); !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Fatalf("missing provider: got %v want invalid input", err)
	}
	if err := service.SetOverride(ctx, models.ModelOverride{ProviderID: "mock"}); !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Fatalf("missing name: got %v want invalid input", err)
	}
}

func TestGormOverrideForeignKey(t *testing.T) {
	service, ctx, database := setupGormModelInfo(t)
	override := models.ModelOverride{ProviderID: "missing-provider", Name: "x", Disabled: true}
	if err := service.SetOverride(ctx, override); !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Fatalf("dangling provider set: got %v want invalid input", err)
	}
	dangling := record.ModelOverride{ProviderID: "missing-provider", ModelName: "x"}
	if err := gorm.G[record.ModelOverride](database.Handle).Create(ctx, &dangling); !stdErrors.Is(db.Translate(err), apierrors.ErrInvalidInput) {
		t.Fatalf("dangling provider insert: got %v want invalid input", err)
	}
}

func TestGormListOverrides(t *testing.T) {
	service, ctx, database := setupGormModelInfo(t)
	seedGormProvider(t, database, ctx, "other", "Other")
	for _, override := range []models.ModelOverride{
		{ProviderID: "mock", Name: "b-model", Capabilities: []string{"tools"}},
		{ProviderID: "mock", Name: "a-model", Disabled: true},
		{ProviderID: "other", Name: "a-model"},
	} {
		if err := service.SetOverride(ctx, override); err != nil {
			t.Fatalf("set %+v: %v", override, err)
		}
	}
	listed, err := service.ListOverrides(ctx, "mock")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 || listed[0].Name != "a-model" || listed[1].Name != "b-model" {
		t.Fatalf("ordered provider set: %+v", listed)
	}
	if !listed[0].Disabled || len(listed[1].Capabilities) != 1 {
		t.Fatalf("child state: %+v", listed)
	}
	empty, err := service.ListOverrides(ctx, "missing-provider")
	if err != nil {
		t.Fatalf("empty list must not error: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty list must yield empty slice, got %+v", empty)
	}
}

func TestGormSetOverrideReplacesChildren(t *testing.T) {
	service, ctx, _ := setupGormModelInfo(t)
	first := models.ModelOverride{
		ProviderID:       "mock",
		Name:             "model",
		Capabilities:     []string{"tools", "json_mode"},
		InputModalities:  []string{"text", "image"},
		OutputModalities: []string{"text"},
	}
	if err := service.SetOverride(ctx, first); err != nil {
		t.Fatalf("first set: %v", err)
	}
	second := models.ModelOverride{
		ProviderID:      "mock",
		Name:            "model",
		Capabilities:    []string{"reasoning"},
		InputModalities: []string{"text"},
	}
	if err := service.SetOverride(ctx, second); err != nil {
		t.Fatalf("second set: %v", err)
	}
	fetched, err := service.GetOverride(ctx, "mock", "model")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(fetched.Capabilities) != 1 || fetched.Capabilities[0] != "reasoning" {
		t.Fatalf("capabilities must replace: %+v", fetched.Capabilities)
	}
	if len(fetched.InputModalities) != 1 || fetched.InputModalities[0] != "text" {
		t.Fatalf("input modalities must replace: %+v", fetched.InputModalities)
	}
	if len(fetched.OutputModalities) != 0 {
		t.Fatalf("cleared modalities must stay cleared: %+v", fetched.OutputModalities)
	}
}

func TestGormDeleteOverride(t *testing.T) {
	service, ctx, database := setupGormModelInfo(t)
	override := models.ModelOverride{ProviderID: "mock", Name: "model", Capabilities: []string{"tools"}, InputModalities: []string{"text"}}
	if err := service.SetOverride(ctx, override); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := service.DeleteOverride(ctx, "mock", "model"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := service.GetOverride(ctx, "mock", "model"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("get after delete: got %v want not found", err)
	}
	if _, err := gorm.G[record.ModelOverrideCapability](database.Handle, clause.Eq{Column: "provider_id", Value: "mock"}, clause.Eq{Column: "model_name", Value: "model"}).First(ctx); !stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
		t.Fatalf("capabilities must cascade on override delete, got %v", err)
	}
	if _, err := gorm.G[record.ModelOverrideModality](database.Handle, clause.Eq{Column: "provider_id", Value: "mock"}, clause.Eq{Column: "model_name", Value: "model"}).First(ctx); !stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
		t.Fatalf("modalities must cascade on override delete, got %v", err)
	}
	if err := service.DeleteOverride(ctx, "mock", "model"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("second delete: got %v want not found", err)
	}
}

func TestGormIsModelEnabled(t *testing.T) {
	service, ctx, _ := setupGormModelInfo(t)
	enabled, err := service.IsModelEnabled(ctx, "mock", "live-model")
	if err != nil || !enabled {
		t.Fatalf("models are enabled by default: %v %v", enabled, err)
	}
	if err := service.SetOverride(ctx, models.ModelOverride{ProviderID: "mock", Name: "live-model", Disabled: true}); err != nil {
		t.Fatalf("set: %v", err)
	}
	enabled, err = service.IsModelEnabled(ctx, "mock", "live-model")
	if err != nil || enabled {
		t.Fatalf("override did not disable the model: %v %v", enabled, err)
	}
	if err := service.DeleteOverride(ctx, "mock", "live-model"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	enabled, err = service.IsModelEnabled(ctx, "mock", "live-model")
	if err != nil || !enabled {
		t.Fatalf("model stays disabled after override removal: %v %v", enabled, err)
	}
}

func TestGormOverrideModalityDirectionCheck(t *testing.T) {
	_, ctx, database := setupGormModelInfo(t)
	bad := record.ModelOverrideModality{ProviderID: "mock", ModelName: "model", Direction: "sideways", Modality: "text"}
	if err := gorm.G[record.ModelOverrideModality](database.Handle).Create(ctx, &bad); !stdErrors.Is(db.Translate(err), apierrors.ErrInvalidInput) {
		t.Fatalf("bad direction insert: got %v want invalid input", err)
	}
}

func TestGormPutGetSnapshotRoundTrip(t *testing.T) {
	service, ctx, _ := setupGormModelInfo(t)
	cachedAt := util.Now()
	modelList := []models.ModelInfo{fullGormModel(), {Name: "beta", DisplayName: "Beta"}}
	if err := service.PutSnapshot(ctx, "mock", modelList, cachedAt); err != nil {
		t.Fatalf("put: %v", err)
	}
	fetched, stamped, err := service.GetSnapshot(ctx, "mock")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !stamped.Equal(cachedAt) {
		t.Fatalf("cached at: got %v want %v", stamped, cachedAt)
	}
	if len(fetched) != 2 || fetched[0].Name != "alpha" || fetched[1].Name != "beta" {
		t.Fatalf("ordered models: %+v", fetched)
	}
	alpha := fetched[0]
	if alpha.DisplayName != "Alpha" || alpha.Description != "first model" {
		t.Fatalf("display fields: %+v", alpha)
	}
	if alpha.RPM != 60 || alpha.TPM != 1000 || alpha.RPD != 100 || alpha.ContextWindow != 4096 || alpha.MaxTokens != 1024 {
		t.Fatalf("limits: %+v", alpha)
	}
	if len(alpha.Capabilities) != 2 || alpha.Capabilities[0] != "json_mode" || alpha.Capabilities[1] != "tools" {
		t.Fatalf("capabilities: %+v", alpha.Capabilities)
	}
	if len(alpha.InputModalities) != 2 || alpha.InputModalities[0] != "image" || alpha.InputModalities[1] != "text" {
		t.Fatalf("input modalities: %+v", alpha.InputModalities)
	}
	if len(alpha.OutputModalities) != 1 || alpha.OutputModalities[0] != "text" {
		t.Fatalf("output modalities: %+v", alpha.OutputModalities)
	}
	if len(alpha.SupportedParameters) != 2 || alpha.SupportedParameters[0] != "tools" || alpha.SupportedParameters[1] != "response_format" {
		t.Fatalf("supported parameters: %+v", alpha.SupportedParameters)
	}
	if alpha.Reasoning == nil || len(alpha.Reasoning.SupportedEfforts) != 2 || alpha.Reasoning.DefaultEffort != "low" || !alpha.Reasoning.DefaultEnabled {
		t.Fatalf("reasoning: %+v", alpha.Reasoning)
	}
	if len(alpha.Endpoints) != 2 || alpha.Endpoints[0] != "chat/completions" || alpha.Endpoints[1] != "embeddings" {
		t.Fatalf("endpoints: %+v", alpha.Endpoints)
	}
	if fetched[1].Reasoning != nil || len(fetched[1].Capabilities) != 0 || len(fetched[1].Endpoints) != 0 {
		t.Fatalf("sparse model must keep empty lists: %+v", fetched[1])
	}
	if _, _, err := service.GetSnapshot(ctx, "missing"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("get miss: got %v want not found", err)
	}
}

func TestGormSnapshotForeignKey(t *testing.T) {
	service, ctx, _ := setupGormModelInfo(t)
	if err := service.PutSnapshot(ctx, "missing-provider", []models.ModelInfo{{Name: "x"}}, util.Now()); !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Fatalf("dangling provider put: got %v want invalid input", err)
	}
}

func TestGormPutSnapshotValidation(t *testing.T) {
	service, ctx, _ := setupGormModelInfo(t)
	if err := service.PutSnapshot(ctx, "", []models.ModelInfo{{Name: "x"}}, util.Now()); !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Fatalf("missing provider: got %v want invalid input", err)
	}
}

func TestGormPutSnapshotReplaces(t *testing.T) {
	service, ctx, _ := setupGormModelInfo(t)
	first := []models.ModelInfo{
		{Name: "old-model", DisplayName: "Old", ContextWindow: 100, Capabilities: []string{"tools"}, Endpoints: []string{"chat/completions"}},
		{Name: "gone-model", DisplayName: "Gone", InputModalities: []string{"text"}},
	}
	if err := service.PutSnapshot(ctx, "mock", first, util.Now()); err != nil {
		t.Fatalf("first put: %v", err)
	}
	second := []models.ModelInfo{
		{Name: "old-model", DisplayName: "Changed", ContextWindow: 999, Capabilities: []string{"reasoning"}},
		{Name: "new-model", DisplayName: "New", ContextWindow: 50},
	}
	if err := service.PutSnapshot(ctx, "mock", second, util.Now()); err != nil {
		t.Fatalf("second put: %v", err)
	}
	fetched, _, err := service.GetSnapshot(ctx, "mock")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(fetched) != 2 {
		t.Fatalf("replaced list: got %+v", fetched)
	}
	byName := map[string]models.ModelInfo{}
	for _, model := range fetched {
		byName[model.Name] = model
	}
	old, ok := byName["old-model"]
	if !ok || old.DisplayName != "Changed" || old.ContextWindow != 999 {
		t.Fatalf("existing row must take the fresh values: %+v", byName)
	}
	if len(old.Capabilities) != 1 || old.Capabilities[0] != "reasoning" {
		t.Fatalf("capabilities must replace: %+v", old.Capabilities)
	}
	if len(old.Endpoints) != 0 {
		t.Fatalf("cleared endpoints must stay cleared: %+v", old.Endpoints)
	}
	if _, ok := byName["new-model"]; !ok {
		t.Fatalf("new model not appended: %+v", fetched)
	}
	if _, ok := byName["gone-model"]; ok {
		t.Fatalf("dropped model must disappear: %+v", fetched)
	}
}

func TestGormPutSnapshotEmpty(t *testing.T) {
	service, ctx, _ := setupGormModelInfo(t)
	cachedAt := util.Now()
	if err := service.PutSnapshot(ctx, "mock", nil, cachedAt); err != nil {
		t.Fatalf("put empty: %v", err)
	}
	fetched, stamped, err := service.GetSnapshot(ctx, "mock")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(fetched) != 0 {
		t.Fatalf("empty snapshot must yield empty slice, got %+v", fetched)
	}
	if !stamped.Equal(cachedAt) {
		t.Fatalf("cached at: got %v want %v", stamped, cachedAt)
	}
}

func TestGormRemoveModel(t *testing.T) {
	service, ctx, database := setupGormModelInfo(t)
	modelList := []models.ModelInfo{
		{Name: "gone-model", Capabilities: []string{"tools"}, InputModalities: []string{"text"}, Endpoints: []string{"embeddings"}},
		{Name: "stays-model"},
	}
	if err := service.PutSnapshot(ctx, "mock", modelList, util.Now()); err != nil {
		t.Fatalf("put: %v", err)
	}
	removed, err := service.RemoveModel(ctx, "mock", "gone-model")
	if err != nil || !removed {
		t.Fatalf("remove: removed=%v err=%v", removed, err)
	}
	fetched, _, err := service.GetSnapshot(ctx, "mock")
	if err != nil || len(fetched) != 1 || fetched[0].Name != "stays-model" {
		t.Fatalf("store after remove: %+v err=%v", fetched, err)
	}
	if _, err := gorm.G[record.ModelInfoCapability](database.Handle, clause.Eq{Column: "provider_id", Value: "mock"}, clause.Eq{Column: "model_name", Value: "gone-model"}).First(ctx); !stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
		t.Fatalf("capabilities must cascade on model remove, got %v", err)
	}
	if _, err := gorm.G[record.ModelInfoModality](database.Handle, clause.Eq{Column: "provider_id", Value: "mock"}, clause.Eq{Column: "model_name", Value: "gone-model"}).First(ctx); !stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
		t.Fatalf("modalities must cascade on model remove, got %v", err)
	}
	if _, err := gorm.G[record.ModelInfoEndpoint](database.Handle, clause.Eq{Column: "provider_id", Value: "mock"}, clause.Eq{Column: "model_name", Value: "gone-model"}).First(ctx); !stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
		t.Fatalf("endpoints must cascade on model remove, got %v", err)
	}
	removed, err = service.RemoveModel(ctx, "mock", "gone-model")
	if err != nil || removed {
		t.Fatalf("second remove: removed=%v err=%v", removed, err)
	}
	removed, err = service.RemoveModel(ctx, "missing-provider", "x")
	if err != nil || removed {
		t.Fatalf("missing provider: removed=%v err=%v", removed, err)
	}
}

func TestGormDeleteSnapshot(t *testing.T) {
	service, ctx, database := setupGormModelInfo(t)
	if err := service.PutSnapshot(ctx, "mock", []models.ModelInfo{fullGormModel()}, util.Now()); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := service.DeleteSnapshot(ctx, "mock"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, _, err := service.GetSnapshot(ctx, "mock"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("get after delete: got %v want not found", err)
	}
	if _, err := gorm.G[record.ModelInfo](database.Handle, clause.Eq{Column: "provider_id", Value: "mock"}).First(ctx); !stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
		t.Fatalf("models must cascade on snapshot delete, got %v", err)
	}
	if err := service.DeleteSnapshot(ctx, "mock"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("second delete: got %v want not found", err)
	}
}

func TestGormModelInfoConvertRoundTrip(t *testing.T) {
	model := fullGormModel()
	row, capabilities, modalities, endpoints, err := modelInfoRecordFromModel("mock", model)
	if err != nil {
		t.Fatalf("record from model: %v", err)
	}
	if row.ProviderID != "mock" || row.ModelName != "alpha" || row.RequestsPerMinute != 60 || row.TokensPerMinute != 1000 || row.RequestsPerDay != 100 {
		t.Fatalf("scalar columns: %+v", row)
	}
	if len(capabilities) != 2 || len(modalities) != 3 || len(endpoints) != 2 {
		t.Fatalf("child counts: caps=%d modalities=%d endpoints=%d", len(capabilities), len(modalities), len(endpoints))
	}
	rebuilt, err := modelInfoFromRecord(row,
		[]string{"json_mode", "tools"},
		[]string{"image", "text"},
		[]string{"text"},
		[]string{"chat/completions", "embeddings"})
	if err != nil {
		t.Fatalf("model from record: %v", err)
	}
	if rebuilt.Name != "alpha" || rebuilt.RPM != 60 || len(rebuilt.SupportedParameters) != 2 {
		t.Fatalf("rebuilt scalars: %+v", rebuilt)
	}
	if rebuilt.Reasoning == nil || rebuilt.Reasoning.DefaultEffort != "low" {
		t.Fatalf("rebuilt reasoning: %+v", rebuilt.Reasoning)
	}
	if _, err := modelInfoFromRecord(record.ModelInfo{Reasoning: []byte("{broken")}, nil, nil, nil, nil); err == nil {
		t.Fatal("corrupt reasoning must fail")
	}
	duplicated := models.ModelInfo{Name: "x", Capabilities: []string{"tools", "tools", "reasoning"}}
	duplicatedRow, duplicatedCaps, _, _, err := modelInfoRecordFromModel("mock", duplicated)
	if err != nil {
		t.Fatalf("duplicated record: %v", err)
	}
	_ = duplicatedRow
	if len(duplicatedCaps) != 2 || duplicatedCaps[0].Capability != "reasoning" || duplicatedCaps[1].Capability != "tools" {
		t.Fatalf("capabilities must deduplicate and sort: %+v", duplicatedCaps)
	}
	base := modelOverrideRecordFromModel(models.ModelOverride{ProviderID: "mock", Name: "x", Disabled: true, Custom: true, DisplayName: "X"})
	if base.ProviderID != "mock" || base.ModelName != "x" || !base.Disabled || !base.Custom || base.DisplayName != "X" {
		t.Fatalf("override base columns: %+v", base)
	}
	rebuiltOverride := modelOverrideFromRecords(base, []string{"tools"}, []string{"text"}, nil)
	if rebuiltOverride.Name != "x" || len(rebuiltOverride.Capabilities) != 1 || len(rebuiltOverride.OutputModalities) != 0 {
		t.Fatalf("rebuilt override: %+v", rebuiltOverride)
	}
}

func TestGormMethodsFailClosedWithoutHandle(t *testing.T) {
	service := NewGorm(nil)
	ctx := context.Background()
	if err := service.SetOverride(ctx, models.ModelOverride{ProviderID: "mock", Name: "x"}); err == nil {
		t.Error("set override without handle must fail")
	}
	if _, err := service.GetOverride(ctx, "mock", "x"); err == nil {
		t.Error("get override without handle must fail")
	}
	if err := service.DeleteOverride(ctx, "mock", "x"); err == nil {
		t.Error("delete override without handle must fail")
	}
	if _, err := service.ListOverrides(ctx, "mock"); err == nil {
		t.Error("list overrides without handle must fail")
	}
	if _, err := service.IsModelEnabled(ctx, "mock", "x"); err == nil {
		t.Error("is enabled without handle must fail")
	}
	if err := service.PutSnapshot(ctx, "mock", nil, util.Now()); err == nil {
		t.Error("put snapshot without handle must fail")
	}
	if _, _, err := service.GetSnapshot(ctx, "mock"); err == nil {
		t.Error("get snapshot without handle must fail")
	}
	if err := service.DeleteSnapshot(ctx, "mock"); err == nil {
		t.Error("delete snapshot without handle must fail")
	}
	if _, err := service.RemoveModel(ctx, "mock", "x"); err == nil {
		t.Error("remove model without handle must fail")
	}
}
