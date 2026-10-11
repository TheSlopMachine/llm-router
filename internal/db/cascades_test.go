package db_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"gorm.io/gorm/clause"
)

func openCascadeDatabase(t *testing.T) *db.Database {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cascades.sqlite3")
	database, err := db.OpenSQLite(context.Background(), path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	return database
}

func seedProvider(t *testing.T, ctx context.Context, database *db.Database, id string) {
	t.Helper()
	providers := repository.NewGormRepository[record.ProviderInstance](database.Handle, "id")
	now := time.Now().UTC()
	if err := providers.Upsert(ctx, record.ProviderInstance{ID: id, Name: id, TypeKey: "custom", Settings: map[string]any{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed provider %s: %v", id, err)
	}
}

func seedCredential(t *testing.T, ctx context.Context, database *db.Database, id, providerID string) {
	t.Helper()
	credentials := repository.NewGormRepository[record.Credential](database.Handle, "id")
	now := time.Now().UTC()
	if err := credentials.Upsert(ctx, record.Credential{ID: id, ProviderID: providerID, Data: map[string]any{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed credential %s: %v", id, err)
	}
}

func countRows[T any](t *testing.T, ctx context.Context, database *db.Database, keyColumns ...string) int64 {
	t.Helper()
	store := repository.NewGormRepository[T](database.Handle, keyColumns...)
	count, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func TestProviderDeleteCascades(t *testing.T) {
	ctx := context.Background()
	database := openCascadeDatabase(t)
	seedProvider(t, ctx, database, "provider")
	seedCredential(t, ctx, database, "credential", "provider")
	now := time.Now().UTC()

	parks := repository.NewGormRepository[record.CredentialPark](database.Handle, "credential_id")
	if err := parks.Upsert(ctx, record.CredentialPark{CredentialID: "credential", Until: now.Add(time.Hour), ParkedAt: now}); err != nil {
		t.Fatalf("seed park: %v", err)
	}
	health := repository.NewGormRepository[record.CredentialHealth](database.Handle, "credential_id")
	if err := health.Upsert(ctx, record.CredentialHealth{CredentialID: "credential", LastCheckAt: now}); err != nil {
		t.Fatalf("seed health: %v", err)
	}
	overrides := repository.NewGormRepository[record.ModelOverride](database.Handle, "provider_id", "model_name")
	if err := overrides.Upsert(ctx, record.ModelOverride{ProviderID: "provider", ModelName: "model"}); err != nil {
		t.Fatalf("seed override: %v", err)
	}
	overrideCapabilities := repository.NewGormRepository[record.ModelOverrideCapability](database.Handle, "provider_id", "model_name", "capability")
	if err := overrideCapabilities.Upsert(ctx, record.ModelOverrideCapability{ProviderID: "provider", ModelName: "model", Capability: "tools"}); err != nil {
		t.Fatalf("seed override capability: %v", err)
	}
	snapshots := repository.NewGormRepository[record.ModelInfoSnapshot](database.Handle, "provider_id")
	if err := snapshots.Upsert(ctx, record.ModelInfoSnapshot{ProviderID: "provider", CachedAt: now}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	infos := repository.NewGormRepository[record.ModelInfo](database.Handle, "provider_id", "model_name")
	if err := infos.Upsert(ctx, record.ModelInfo{ProviderID: "provider", ModelName: "model"}); err != nil {
		t.Fatalf("seed info: %v", err)
	}
	infoCapabilities := repository.NewGormRepository[record.ModelInfoCapability](database.Handle, "provider_id", "model_name", "capability")
	if err := infoCapabilities.Upsert(ctx, record.ModelInfoCapability{ProviderID: "provider", ModelName: "model", Capability: "tools"}); err != nil {
		t.Fatalf("seed info capability: %v", err)
	}
	virtualModels := repository.NewGormRepository[record.VirtualModel](database.Handle, "id")
	managedID := "provider"
	if err := virtualModels.Upsert(ctx, record.VirtualModel{ID: "managed", Name: "Managed", NameKey: "managed", ManagedProviderID: &managedID, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed managed virtual: %v", err)
	}
	if err := virtualModels.Upsert(ctx, record.VirtualModel{ID: "manual", Name: "Manual", NameKey: "manual", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed manual virtual: %v", err)
	}
	members := repository.NewGormRepository[record.VirtualModelMember](database.Handle, "virtual_model_id", "position")
	if err := members.Upsert(ctx, record.VirtualModelMember{VirtualModelID: "manual", Position: 0, ProviderID: "provider", ModelName: "model"}); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	providers := repository.NewGormRepository[record.ProviderInstance](database.Handle, "id")
	if _, err := providers.Delete(ctx, clause.Eq{Column: "id", Value: "provider"}); err != nil {
		t.Fatalf("delete provider: %v", err)
	}

	if got := countRows[record.Credential](t, ctx, database, "id"); got != 0 {
		t.Errorf("credentials hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.CredentialPark](t, ctx, database, "credential_id"); got != 0 {
		t.Errorf("parks hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.CredentialHealth](t, ctx, database, "credential_id"); got != 0 {
		t.Errorf("health hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.ModelOverride](t, ctx, database, "provider_id", "model_name"); got != 0 {
		t.Errorf("overrides hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.ModelOverrideCapability](t, ctx, database, "provider_id", "model_name", "capability"); got != 0 {
		t.Errorf("override capabilities hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.ModelInfoSnapshot](t, ctx, database, "provider_id"); got != 0 {
		t.Errorf("snapshots hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.ModelInfo](t, ctx, database, "provider_id", "model_name"); got != 0 {
		t.Errorf("infos hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.ModelInfoCapability](t, ctx, database, "provider_id", "model_name", "capability"); got != 0 {
		t.Errorf("info capabilities hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.VirtualModel](t, ctx, database, "id"); got != 1 {
		t.Errorf("virtual models hold %d rows, want 1 (managed cascaded, manual kept)", got)
	}
	if got := countRows[record.VirtualModelMember](t, ctx, database, "virtual_model_id", "position"); got != 0 {
		t.Errorf("members hold %d rows, want 0 cascade", got)
	}
}

func TestTokenDeleteCascadesRules(t *testing.T) {
	ctx := context.Background()
	database := openCascadeDatabase(t)
	seedProvider(t, ctx, database, "provider")
	seedCredential(t, ctx, database, "credential", "provider")
	now := time.Now().UTC()

	tokens := repository.NewGormRepository[record.RouterToken](database.Handle, "id")
	if err := tokens.Upsert(ctx, record.RouterToken{ID: "token", Name: "token", TokenHash: "hash", CreatedAt: now}); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	allowedProviders := repository.NewGormRepository[record.TokenAllowedProvider](database.Handle, "token_id", "provider_id")
	if err := allowedProviders.Upsert(ctx, record.TokenAllowedProvider{TokenID: "token", ProviderID: "provider"}); err != nil {
		t.Fatalf("seed allowed provider: %v", err)
	}
	allowedModels := repository.NewGormRepository[record.TokenAllowedModel](database.Handle, "token_id", "model_id")
	if err := allowedModels.Upsert(ctx, record.TokenAllowedModel{TokenID: "token", ModelID: "provider/model"}); err != nil {
		t.Fatalf("seed allowed model: %v", err)
	}
	allowedCredentials := repository.NewGormRepository[record.TokenAllowedCredential](database.Handle, "token_id", "credential_id")
	if err := allowedCredentials.Upsert(ctx, record.TokenAllowedCredential{TokenID: "token", CredentialID: "credential"}); err != nil {
		t.Fatalf("seed allowed credential: %v", err)
	}
	scopes := repository.NewGormRepository[record.TokenCredentialScope](database.Handle, "id")
	if err := scopes.Upsert(ctx, record.TokenCredentialScope{ID: "scope", TokenID: "token", ProviderID: "provider", AllCredentials: true}); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	scopeCredentials := repository.NewGormRepository[record.TokenScopeCredential](database.Handle, "scope_id", "credential_id")
	if err := scopeCredentials.Upsert(ctx, record.TokenScopeCredential{ScopeID: "scope", CredentialID: "credential"}); err != nil {
		t.Fatalf("seed scope credential: %v", err)
	}

	if _, err := tokens.Delete(ctx, clause.Eq{Column: "id", Value: "token"}); err != nil {
		t.Fatalf("delete token: %v", err)
	}
	if got := countRows[record.TokenAllowedProvider](t, ctx, database, "token_id", "provider_id"); got != 0 {
		t.Errorf("allowed providers hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.TokenAllowedModel](t, ctx, database, "token_id", "model_id"); got != 0 {
		t.Errorf("allowed models hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.TokenAllowedCredential](t, ctx, database, "token_id", "credential_id"); got != 0 {
		t.Errorf("allowed credentials hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.TokenCredentialScope](t, ctx, database, "id"); got != 0 {
		t.Errorf("scopes hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.TokenScopeCredential](t, ctx, database, "scope_id", "credential_id"); got != 0 {
		t.Errorf("scope credentials hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.Credential](t, ctx, database, "id"); got != 1 {
		t.Errorf("credentials hold %d rows, want 1 (rules cascade, rows kept)", got)
	}
}

func TestCredentialDeleteCascadesTokenRules(t *testing.T) {
	ctx := context.Background()
	database := openCascadeDatabase(t)
	seedProvider(t, ctx, database, "provider")
	seedCredential(t, ctx, database, "credential", "provider")
	now := time.Now().UTC()

	tokens := repository.NewGormRepository[record.RouterToken](database.Handle, "id")
	if err := tokens.Upsert(ctx, record.RouterToken{ID: "token", Name: "token", TokenHash: "hash", CreatedAt: now}); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	allowedCredentials := repository.NewGormRepository[record.TokenAllowedCredential](database.Handle, "token_id", "credential_id")
	if err := allowedCredentials.Upsert(ctx, record.TokenAllowedCredential{TokenID: "token", CredentialID: "credential"}); err != nil {
		t.Fatalf("seed allowed credential: %v", err)
	}

	credentials := repository.NewGormRepository[record.Credential](database.Handle, "id")
	if _, err := credentials.Delete(ctx, clause.Eq{Column: "id", Value: "credential"}); err != nil {
		t.Fatalf("delete credential: %v", err)
	}
	if got := countRows[record.TokenAllowedCredential](t, ctx, database, "token_id", "credential_id"); got != 0 {
		t.Errorf("allowed credentials hold %d rows, want 0 cascade", got)
	}
}

func TestPluginDeleteCascades(t *testing.T) {
	ctx := context.Background()
	database := openCascadeDatabase(t)
	now := time.Now().UTC()
	plugins := repository.NewGormRepository[record.Plugin](database.Handle, "id")
	if err := plugins.Upsert(ctx, record.Plugin{ID: "plugin", DisplayName: "Plugin", Author: "author", Version: "1.0.0", PluginAPIVersion: "1.1", Source: []byte("return {}"), Declarations: map[string]any{}, InstalledAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed plugin: %v", err)
	}
	hosts := repository.NewGormRepository[record.PluginAllowHost](database.Handle, "plugin_id", "host")
	if err := hosts.Upsert(ctx, record.PluginAllowHost{PluginID: "plugin", Host: "example.com"}); err != nil {
		t.Fatalf("seed host: %v", err)
	}
	versions := repository.NewGormRepository[record.PluginVersion](database.Handle, "id")
	if err := versions.Upsert(ctx, record.PluginVersion{ID: "version", PluginID: "plugin", Version: "1.0.0", Source: []byte("return {}"), Declarations: map[string]any{}, CreatedAt: now}); err != nil {
		t.Fatalf("seed version: %v", err)
	}
	storage := repository.NewGormRepository[record.PluginStorage](database.Handle, "plugin_id", "scope", "key")
	if err := storage.Upsert(ctx, record.PluginStorage{PluginID: "plugin", Scope: "scope", Key: "key", Value: []byte("{}")}); err != nil {
		t.Fatalf("seed storage: %v", err)
	}

	if _, err := plugins.Delete(ctx, clause.Eq{Column: "id", Value: "plugin"}); err != nil {
		t.Fatalf("delete plugin: %v", err)
	}
	if got := countRows[record.PluginAllowHost](t, ctx, database, "plugin_id", "host"); got != 0 {
		t.Errorf("allow hosts hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.PluginVersion](t, ctx, database, "id"); got != 0 {
		t.Errorf("versions hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.PluginStorage](t, ctx, database, "plugin_id", "scope", "key"); got != 0 {
		t.Errorf("storage holds %d rows, want 0 cascade", got)
	}
}

func TestPoolRestrictBlocksReferencedDelete(t *testing.T) {
	ctx := context.Background()
	database := openCascadeDatabase(t)
	now := time.Now().UTC()
	pools := repository.NewGormRepository[record.ProxyPool](database.Handle, "id")
	if err := pools.Upsert(ctx, record.ProxyPool{ID: "pool", Name: "pool", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
	providers := repository.NewGormRepository[record.ProviderInstance](database.Handle, "id")
	poolID := "pool"
	if err := providers.Upsert(ctx, record.ProviderInstance{ID: "provider", Name: "provider", TypeKey: "custom", ProxyPoolID: &poolID, Settings: map[string]any{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed provider: %v", err)
	}

	if _, err := pools.Delete(ctx, clause.Eq{Column: "id", Value: "pool"}); !errors.Is(db.Translate(err), apierrors.ErrInvalidInput) {
		t.Errorf("referenced pool delete returns %v, want invalid input", err)
	}
	if _, err := providers.Delete(ctx, clause.Eq{Column: "id", Value: "provider"}); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	if _, err := pools.Delete(ctx, clause.Eq{Column: "id", Value: "pool"}); err != nil {
		t.Errorf("unreferenced pool delete returns %v, want success", err)
	}
}

func TestRepositoryDeleteNullsPluginOrigin(t *testing.T) {
	ctx := context.Background()
	database := openCascadeDatabase(t)
	now := time.Now().UTC()
	repositories := repository.NewGormRepository[record.PluginRepository](database.Handle, "id")
	if err := repositories.Upsert(ctx, record.PluginRepository{ID: "repo", Kind: "index", IndexURL: "https://example.com/index.json", AddedAt: now}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	plugins := repository.NewGormRepository[record.Plugin](database.Handle, "id")
	repoID := "repo"
	if err := plugins.Upsert(ctx, record.Plugin{ID: "plugin", DisplayName: "Plugin", Author: "author", Version: "1.0.0", PluginAPIVersion: "1.1", OriginRepositoryID: &repoID, Source: []byte("return {}"), Declarations: map[string]any{}, InstalledAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed plugin: %v", err)
	}

	if _, err := repositories.Delete(ctx, clause.Eq{Column: "id", Value: "repo"}); err != nil {
		t.Fatalf("delete repo: %v", err)
	}
	row, err := plugins.Get(ctx, clause.Eq{Column: "id", Value: "plugin"})
	if err != nil {
		t.Fatalf("get plugin: %v", err)
	}
	if row.OriginRepositoryID != nil {
		t.Errorf("origin repository is %q, want null after repo delete", *row.OriginRepositoryID)
	}
}

func TestMetricRetentionCascadesChildren(t *testing.T) {
	ctx := context.Background()
	database := openCascadeDatabase(t)
	now := time.Now().UTC().Truncate(time.Second)
	buckets := repository.NewGormRepository[record.MetricBucket](database.Handle, "granularity", "timestamp")
	if err := buckets.Upsert(ctx, record.MetricBucket{Granularity: "1m", Timestamp: now}); err != nil {
		t.Fatalf("seed bucket: %v", err)
	}
	providers := repository.NewGormRepository[record.MetricBucketProvider](database.Handle, "granularity", "timestamp", "dimension", "key")
	if err := providers.Upsert(ctx, record.MetricBucketProvider{Granularity: "1m", Timestamp: now, Dimension: "provider_id", Key: "provider"}); err != nil {
		t.Fatalf("seed provider dimension: %v", err)
	}
	models := repository.NewGormRepository[record.MetricBucketModel](database.Handle, "granularity", "timestamp", "model_id")
	if err := models.Upsert(ctx, record.MetricBucketModel{Granularity: "1m", Timestamp: now, ModelID: "provider/model"}); err != nil {
		t.Fatalf("seed model dimension: %v", err)
	}
	tokens := repository.NewGormRepository[record.MetricBucketToken](database.Handle, "granularity", "timestamp", "token_id")
	if err := tokens.Upsert(ctx, record.MetricBucketToken{Granularity: "1m", Timestamp: now, TokenID: "token"}); err != nil {
		t.Fatalf("seed token dimension: %v", err)
	}
	errorTypes := repository.NewGormRepository[record.MetricBucketErrorType](database.Handle, "granularity", "timestamp", "error_type")
	if err := errorTypes.Upsert(ctx, record.MetricBucketErrorType{Granularity: "1m", Timestamp: now, ErrorType: "upstream_error", Count: 1}); err != nil {
		t.Fatalf("seed error dimension: %v", err)
	}

	if _, err := buckets.Delete(ctx, clause.Eq{Column: "granularity", Value: "1m"}, clause.Eq{Column: "timestamp", Value: now}); err != nil {
		t.Fatalf("delete bucket: %v", err)
	}
	if got := countRows[record.MetricBucketProvider](t, ctx, database, "granularity", "timestamp", "dimension", "key"); got != 0 {
		t.Errorf("provider dimensions hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.MetricBucketModel](t, ctx, database, "granularity", "timestamp", "model_id"); got != 0 {
		t.Errorf("model dimensions hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.MetricBucketToken](t, ctx, database, "granularity", "timestamp", "token_id"); got != 0 {
		t.Errorf("token dimensions hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.MetricBucketErrorType](t, ctx, database, "granularity", "timestamp", "error_type"); got != 0 {
		t.Errorf("error dimensions hold %d rows, want 0 cascade", got)
	}
}

func TestBatchDeleteCascadesEntries(t *testing.T) {
	ctx := context.Background()
	database := openCascadeDatabase(t)
	now := time.Now().UTC()
	batches := repository.NewGormRepository[record.MessageBatch](database.Handle, "id")
	if err := batches.Upsert(ctx, record.MessageBatch{ID: "batch", Model: "provider/model", Status: "ended", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed batch: %v", err)
	}
	entries := repository.NewGormRepository[record.MessageBatchEntry](database.Handle, "batch_id", "position")
	if err := entries.Upsert(ctx, record.MessageBatchEntry{BatchID: "batch", Position: 0, CustomID: "request", Params: []byte("{}")}); err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	results := repository.NewGormRepository[record.MessageBatchResult](database.Handle, "batch_id", "position")
	if err := results.Upsert(ctx, record.MessageBatchResult{BatchID: "batch", Position: 0, CustomID: "request", Result: []byte("{}")}); err != nil {
		t.Fatalf("seed result: %v", err)
	}

	if _, err := batches.Delete(ctx, clause.Eq{Column: "id", Value: "batch"}); err != nil {
		t.Fatalf("delete batch: %v", err)
	}
	if got := countRows[record.MessageBatchEntry](t, ctx, database, "batch_id", "position"); got != 0 {
		t.Errorf("entries hold %d rows, want 0 cascade", got)
	}
	if got := countRows[record.MessageBatchResult](t, ctx, database, "batch_id", "position"); got != 0 {
		t.Errorf("results hold %d rows, want 0 cascade", got)
	}
}
