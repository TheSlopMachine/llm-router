package migrations

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/migrate"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func openLegacyHandle(t *testing.T, path string) *gorm.DB {
	t.Helper()
	handle, err := gorm.Open(sqlite.Open("file:"+path+"?_pragma=foreign_keys(1)"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open legacy handle: %v", err)
	}
	sqlDatabase, err := handle.DB()
	if err != nil {
		t.Fatalf("resolve sql handle: %v", err)
	}
	t.Cleanup(func() {
		sqlDatabase.Close()
	})
	return handle
}

func seedLegacyDatabase(t *testing.T, ctx context.Context, primary *gorm.DB, now time.Time) {
	t.Helper()
	if err := migrate.Run(ctx, primary, RegistryDatabase[:1]); err != nil {
		t.Fatalf("apply version 1: %v", err)
	}
	users := []adminUserSnapshot{{Username: "operator", PasswordHash: "hash", CreatedAt: now}}
	if err := primary.WithContext(ctx).Create(&users).Error; err != nil {
		t.Fatalf("seed admin user: %v", err)
	}
	sessions := []sessionSnapshot{{TokenHash: "hash", Username: "operator", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}}
	if err := primary.WithContext(ctx).Create(&sessions).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	states := []proxyStateSnapshot{{URL: "http://proxy:8080", IP: "10.0.0.1", Port: 8080, LastSeenInSource: now, ReviveAt: now, LastCheckedAt: now}}
	if err := primary.WithContext(ctx).Create(&states).Error; err != nil {
		t.Fatalf("seed proxy state: %v", err)
	}
	providers := []providerInstanceSnapshot{{ID: "provider", Name: "provider", TypeKey: "custom", CreatedAt: now, UpdatedAt: now}}
	if err := primary.WithContext(ctx).Create(&providers).Error; err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	credentials := []credentialSnapshot{{ID: "credential", ProviderID: "provider", CreatedAt: now, UpdatedAt: now}}
	if err := primary.WithContext(ctx).Create(&credentials).Error; err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	parks := []credentialParkSnapshot{{CredentialID: "credential", Reason: "rate", Until: now.Add(time.Hour), ParkedAt: now}}
	if err := primary.WithContext(ctx).Create(&parks).Error; err != nil {
		t.Fatalf("seed park: %v", err)
	}
	health := []credentialHealthSnapshot{{CredentialID: "credential", LastCheckAt: now, LastStatus: "healthy"}}
	if err := primary.WithContext(ctx).Create(&health).Error; err != nil {
		t.Fatalf("seed health: %v", err)
	}
	snapshotHeaders := []modelInfoSnapshotSnapshot{{ProviderID: "provider", CachedAt: now}}
	if err := primary.WithContext(ctx).Create(&snapshotHeaders).Error; err != nil {
		t.Fatalf("seed snapshot header: %v", err)
	}
	infos := []modelInfoSnapshot{{ProviderID: "provider", ModelName: "model"}}
	if err := primary.WithContext(ctx).Create(&infos).Error; err != nil {
		t.Fatalf("seed model info: %v", err)
	}
	capabilities := []modelInfoCapabilitySnapshot{{ProviderID: "provider", ModelName: "model", Capability: "tools"}}
	if err := primary.WithContext(ctx).Create(&capabilities).Error; err != nil {
		t.Fatalf("seed model capability: %v", err)
	}
	modalities := []modelInfoModalitySnapshot{{ProviderID: "provider", ModelName: "model", Direction: "input", Modality: "text"}}
	if err := primary.WithContext(ctx).Create(&modalities).Error; err != nil {
		t.Fatalf("seed model modality: %v", err)
	}
	endpoints := []modelInfoEndpointSnapshot{{ProviderID: "provider", ModelName: "model", Endpoint: "chat"}}
	if err := primary.WithContext(ctx).Create(&endpoints).Error; err != nil {
		t.Fatalf("seed model endpoint: %v", err)
	}
	plugins := []pluginSnapshot{{ID: "plugin", DisplayName: "Plugin", Author: "author", Version: "1.0.0", PluginAPIVersion: "1.2", Source: []byte("return {}"), InstalledAt: now, UpdatedAt: now}}
	if err := primary.WithContext(ctx).Create(&plugins).Error; err != nil {
		t.Fatalf("seed plugin: %v", err)
	}
	expiry := now.Add(time.Hour)
	storage := []pluginStorageSnapshot{
		{PluginID: "plugin", Scope: "scope", Key: "ephemeral", Value: []byte(`"hot"`), ExpiresAt: &expiry},
		{PluginID: "plugin", Scope: "scope", Key: "durable", Value: []byte(`"cold"`)},
	}
	if err := primary.WithContext(ctx).Create(&storage).Error; err != nil {
		t.Fatalf("seed plugin storage: %v", err)
	}
	buckets := []metricBucketSnapshot{{Granularity: "1m", Timestamp: now}}
	if err := primary.WithContext(ctx).Create(&buckets).Error; err != nil {
		t.Fatalf("seed metric bucket: %v", err)
	}
	bucketProviders := []metricBucketProviderSnapshot{{Granularity: "1m", Timestamp: now, Dimension: "provider_id", Key: "provider"}}
	if err := primary.WithContext(ctx).Create(&bucketProviders).Error; err != nil {
		t.Fatalf("seed metric provider: %v", err)
	}
	videos := []videoJobSnapshot{{ID: "video", Model: "provider/model", BackendModel: "model", ProviderID: "provider", TypeKey: "custom", UpstreamJobID: "upstream", Status: "queued", CreatedAt: now, UpdatedAt: now}}
	if err := primary.WithContext(ctx).Create(&videos).Error; err != nil {
		t.Fatalf("seed video job: %v", err)
	}
	responses := []responseRecordSnapshot{{ID: "response", Kind: "response", Model: "provider/model", Status: "done", RefID: "ref", Payload: []byte("{}"), Input: []byte("{}"), CreatedAt: now, UpdatedAt: now}}
	if err := primary.WithContext(ctx).Create(&responses).Error; err != nil {
		t.Fatalf("seed response record: %v", err)
	}
	batches := []messageBatchSnapshot{{ID: "batch", Model: "provider/model", Status: "ended", CreatedAt: now, UpdatedAt: now}}
	if err := primary.WithContext(ctx).Create(&batches).Error; err != nil {
		t.Fatalf("seed message batch: %v", err)
	}
	entries := []messageBatchEntrySnapshot{{BatchID: "batch", Position: 0, CustomID: "request", Params: []byte("{}")}}
	if err := primary.WithContext(ctx).Create(&entries).Error; err != nil {
		t.Fatalf("seed batch entry: %v", err)
	}
	results := []messageBatchResultSnapshot{{BatchID: "batch", Position: 0, CustomID: "request", Result: []byte("{}")}}
	if err := primary.WithContext(ctx).Create(&results).Error; err != nil {
		t.Fatalf("seed batch result: %v", err)
	}
}

func countCacheRows[Row any](t *testing.T, ctx context.Context, cache *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := cache.WithContext(ctx).Model(new(Row)).Count(&count).Error; err != nil {
		t.Fatalf("count cache rows: %v", err)
	}
	return count
}

func TestSplitStorageUpgradesLegacyFile(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	directory := t.TempDir()
	primary := openLegacyHandle(t, filepath.Join(directory, "legacy.sqlite3"))
	cache := openLegacyHandle(t, filepath.Join(directory, "legacy.cache.sqlite3"))
	seedLegacyDatabase(t, ctx, primary, now)

	if !LegacySingleFile(primary) {
		t.Fatalf("legacy file not detected before split")
	}
	if err := migrate.Run(ctx, cache, RegistryCache); err != nil {
		t.Fatalf("migrate cache: %v", err)
	}
	if err := CopyLegacyCacheTables(ctx, primary, cache); err != nil {
		t.Fatalf("copy legacy cache tables: %v", err)
	}
	if got := countCacheRows[cacheSession](t, ctx, cache); got != 1 {
		t.Errorf("cache sessions hold %d rows, want 1", got)
	}
	if got := countCacheRows[cacheProxyState](t, ctx, cache); got != 1 {
		t.Errorf("cache proxy states hold %d rows, want 1", got)
	}
	if got := countCacheRows[cacheCredentialPark](t, ctx, cache); got != 1 {
		t.Errorf("cache parks hold %d rows, want 1", got)
	}
	if got := countCacheRows[cacheCredentialHealth](t, ctx, cache); got != 1 {
		t.Errorf("cache health hold %d rows, want 1", got)
	}
	if got := countCacheRows[cacheModelInfoSnapshot](t, ctx, cache); got != 1 {
		t.Errorf("cache snapshot headers hold %d rows, want 1", got)
	}
	if got := countCacheRows[cacheModelInfo](t, ctx, cache); got != 1 {
		t.Errorf("cache model infos hold %d rows, want 1", got)
	}
	if got := countCacheRows[cacheModelInfoCapability](t, ctx, cache); got != 1 {
		t.Errorf("cache model capabilities hold %d rows, want 1", got)
	}
	if got := countCacheRows[cacheModelInfoModality](t, ctx, cache); got != 1 {
		t.Errorf("cache model modalities hold %d rows, want 1", got)
	}
	if got := countCacheRows[cacheModelInfoEndpoint](t, ctx, cache); got != 1 {
		t.Errorf("cache model endpoints hold %d rows, want 1", got)
	}
	if got := countCacheRows[cachePluginStorage](t, ctx, cache); got != 1 {
		t.Errorf("cache plugin storage holds %d rows, want 1 ttl row only", got)
	}
	if got := countCacheRows[cacheMetricBucket](t, ctx, cache); got != 1 {
		t.Errorf("cache metric buckets hold %d rows, want 1", got)
	}
	if got := countCacheRows[cacheMetricBucketProvider](t, ctx, cache); got != 1 {
		t.Errorf("cache metric providers hold %d rows, want 1", got)
	}
	if got := countCacheRows[cacheVideoJob](t, ctx, cache); got != 1 {
		t.Errorf("cache video jobs hold %d rows, want 1", got)
	}
	if got := countCacheRows[cacheResponseRecord](t, ctx, cache); got != 1 {
		t.Errorf("cache response records hold %d rows, want 1", got)
	}
	if got := countCacheRows[cacheMessageBatch](t, ctx, cache); got != 1 {
		t.Errorf("cache message batches hold %d rows, want 1", got)
	}
	if got := countCacheRows[cacheMessageBatchEntry](t, ctx, cache); got != 1 {
		t.Errorf("cache batch entries hold %d rows, want 1", got)
	}
	if got := countCacheRows[cacheMessageBatchResult](t, ctx, cache); got != 1 {
		t.Errorf("cache batch results hold %d rows, want 1", got)
	}

	// A repeated copy converges instead of duplicating.
	if err := CopyLegacyCacheTables(ctx, primary, cache); err != nil {
		t.Fatalf("recopy legacy cache tables: %v", err)
	}
	if got := countCacheRows[cacheSession](t, ctx, cache); got != 1 {
		t.Errorf("cache sessions hold %d rows after recopy, want 1", got)
	}

	if err := migrate.Run(ctx, primary, RegistryDatabase); err != nil {
		t.Fatalf("migrate primary: %v", err)
	}
	if LegacySingleFile(primary) {
		t.Errorf("legacy file still detected after split")
	}
	for _, table := range []string{
		"sessions", "proxy_states", "credential_parks", "credential_health",
		"model_info_snapshots", "model_infos", "model_info_capabilities",
		"model_info_modalities", "model_info_endpoints", "metric_buckets",
		"metric_bucket_providers", "video_jobs", "response_records",
		"message_batches", "message_batch_entries", "message_batch_results",
		"plugin_storage",
	} {
		if table == "plugin_storage" {
			if !primary.Migrator().HasTable(table) {
				t.Errorf("primary misses durable table %s after split", table)
			}
			continue
		}
		if primary.Migrator().HasTable(table) {
			t.Errorf("primary still holds moved table %s after split", table)
		}
	}
	var durableStorage int64
	if err := primary.WithContext(ctx).Model(&pluginStorageSnapshot{}).Count(&durableStorage).Error; err != nil {
		t.Fatalf("count durable storage: %v", err)
	}
	if durableStorage != 1 {
		t.Errorf("primary plugin storage holds %d rows, want 1 durable row", durableStorage)
	}
}
