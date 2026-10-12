// Split storage owns migration version 2 plus the cache-file schema.
//
// The durable database keeps version 1 untouched and gains version 2, which
// drops the moved tables and removes TTL plugin storage rows (copied to the
// cache file beforehand by CopyLegacyCacheTables). The cache file runs its
// own registry with its own history table: version 1 creates every cache
// table. Cache snapshots drop cross-file associations: SQLite cannot enforce
// foreign keys across files, so cache rows reference durable parents softly.
// Associations inside the cache file (batch children, metric dimensions,
// model info children) keep their cascade constraints.
package migrations

import (
	"context"
	"fmt"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/migrate"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RegistryCache lists every migration for the cache database file in order.
// Version numbers are scoped per file: each file owns its own history table,
// so the cache lineage starts at 1 independently of the primary lineage.
var RegistryCache = []migrate.Migration{
	{Version: 1, Name: "cache_schema", Up: upCacheSchema},
}

type cacheSession struct {
	TokenHash  string    `gorm:"primaryKey"`
	Username   string    `gorm:"not null;index"`
	CreatedAt  time.Time `gorm:"not null"`
	ExpiresAt  time.Time `gorm:"not null;index"`
	RememberMe bool      `gorm:"not null"`
}

// TableName pins the sessions table name.
func (cacheSession) TableName() string {
	return "sessions"
}

type cacheProxyState struct {
	URL                string            `gorm:"primaryKey"`
	IP                 string            `gorm:"not null"`
	Port               int               `gorm:"not null"`
	Location           string            `gorm:"not null"`
	IsDead             bool              `gorm:"not null;index"`
	Source             string            `gorm:"not null"`
	FailReason         string            `gorm:"not null;check:fail_reason IN ('','timeout','refused','rejected','eof_reset','dns','tls','bad_status','local_net','other')"`
	SoftFails          int               `gorm:"not null"`
	Suspect            bool              `gorm:"not null"`
	LastSeenInSource   time.Time         `gorm:"not null"`
	Penalty            float64           `gorm:"not null"`
	Score              float64           `gorm:"not null"`
	ReviveAt           time.Time         `gorm:"not null"`
	LastCheckedAt      time.Time         `gorm:"not null;index"`
	LatencyNanoseconds int64             `gorm:"not null"`
	Metadata           map[string]string `gorm:"serializer:json"`
}

// TableName pins the proxy states table name.
func (cacheProxyState) TableName() string {
	return "proxy_states"
}

type cacheModelInfoSnapshot struct {
	ProviderID string    `gorm:"primaryKey"`
	CachedAt   time.Time `gorm:"not null"`
}

// TableName pins the model info snapshots table name.
func (cacheModelInfoSnapshot) TableName() string {
	return "model_info_snapshots"
}

type cacheModelInfo struct {
	ProviderID          string                 `gorm:"primaryKey"`
	Snapshot            cacheModelInfoSnapshot `gorm:"foreignKey:ProviderID;references:ProviderID;belongsTo;constraint:OnDelete:CASCADE"`
	ModelName           string                 `gorm:"primaryKey"`
	DisplayName         string                 `gorm:"not null"`
	Description         string                 `gorm:"not null"`
	RequestsPerMinute   int64                  `gorm:"not null"`
	TokensPerMinute     int64                  `gorm:"not null"`
	RequestsPerDay      int64                  `gorm:"not null"`
	ContextWindow       int64                  `gorm:"not null"`
	MaxTokens           int64                  `gorm:"not null"`
	SupportedParameters []string               `gorm:"serializer:json"`
	Reasoning           []byte                 `gorm:""`
}

// TableName pins the model infos table name.
func (cacheModelInfo) TableName() string {
	return "model_infos"
}

type cacheModelInfoCapability struct {
	ProviderID string         `gorm:"primaryKey"`
	ModelName  string         `gorm:"primaryKey"`
	Model      cacheModelInfo `gorm:"foreignKey:ProviderID,ModelName;references:ProviderID,ModelName;belongsTo;constraint:OnDelete:CASCADE"`
	Capability string         `gorm:"primaryKey"`
}

// TableName pins the model info capabilities table name.
func (cacheModelInfoCapability) TableName() string {
	return "model_info_capabilities"
}

type cacheModelInfoModality struct {
	ProviderID string         `gorm:"primaryKey"`
	ModelName  string         `gorm:"primaryKey"`
	Model      cacheModelInfo `gorm:"foreignKey:ProviderID,ModelName;references:ProviderID,ModelName;belongsTo;constraint:OnDelete:CASCADE"`
	Direction  string         `gorm:"primaryKey;check:direction IN ('input','output')"`
	Modality   string         `gorm:"primaryKey"`
}

// TableName pins the model info modalities table name.
func (cacheModelInfoModality) TableName() string {
	return "model_info_modalities"
}

type cacheModelInfoEndpoint struct {
	ProviderID string         `gorm:"primaryKey"`
	ModelName  string         `gorm:"primaryKey"`
	Model      cacheModelInfo `gorm:"foreignKey:ProviderID,ModelName;references:ProviderID,ModelName;belongsTo;constraint:OnDelete:CASCADE"`
	Endpoint   string         `gorm:"primaryKey"`
}

// TableName pins the model info endpoints table name.
func (cacheModelInfoEndpoint) TableName() string {
	return "model_info_endpoints"
}

type cacheCredentialPark struct {
	CredentialID string    `gorm:"primaryKey"`
	Reason       string    `gorm:"not null"`
	Until        time.Time `gorm:"not null;index"`
	ParkedAt     time.Time `gorm:"not null"`
}

// TableName pins the credential parks table name.
func (cacheCredentialPark) TableName() string {
	return "credential_parks"
}

type cacheCredentialHealth struct {
	CredentialID string    `gorm:"primaryKey"`
	LastCheckAt  time.Time `gorm:"not null"`
	LastStatus   string    `gorm:"not null"`
}

// TableName pins the credential health table name.
func (cacheCredentialHealth) TableName() string {
	return "credential_health"
}

type cachePluginStorage struct {
	PluginID  string     `gorm:"primaryKey"`
	Scope     string     `gorm:"primaryKey"`
	Key       string     `gorm:"primaryKey"`
	Value     []byte     `gorm:"not null"`
	ExpiresAt *time.Time `gorm:"index"`
}

// TableName pins the plugin storage table name.
func (cachePluginStorage) TableName() string {
	return "plugin_storage"
}

type cacheMetricBucket struct {
	Granularity             string    `gorm:"primaryKey"`
	Timestamp               time.Time `gorm:"primaryKey;index:idx_metric_buckets_timestamp"`
	TotalRequests           int64     `gorm:"not null"`
	TotalErrors             int64     `gorm:"not null"`
	TokensInput             int64     `gorm:"not null"`
	TokensOutput            int64     `gorm:"not null"`
	PeakRequests            int64     `gorm:"not null"`
	PeakInputTokens         int64     `gorm:"not null"`
	PeakOutputTokens        int64     `gorm:"not null"`
	DurationSumMicroseconds int64     `gorm:"not null"`
	DurationCount           int64     `gorm:"not null"`
}

// TableName pins the metric buckets table name.
func (cacheMetricBucket) TableName() string {
	return "metric_buckets"
}

type cacheMetricBucketProvider struct {
	Granularity      string            `gorm:"primaryKey"`
	Timestamp        time.Time         `gorm:"primaryKey"`
	Bucket           cacheMetricBucket `gorm:"foreignKey:Granularity,Timestamp;references:Granularity,Timestamp;belongsTo;constraint:OnDelete:CASCADE"`
	Dimension        string            `gorm:"primaryKey;check:dimension IN ('provider_id','provider_type')"`
	Key              string            `gorm:"primaryKey"`
	Requests         int64             `gorm:"not null"`
	Errors           int64             `gorm:"not null"`
	TokensInput      int64             `gorm:"not null"`
	TokensOutput     int64             `gorm:"not null"`
	PeakRequests     int64             `gorm:"not null"`
	PeakInputTokens  int64             `gorm:"not null"`
	PeakOutputTokens int64             `gorm:"not null"`
}

// TableName pins the metric bucket providers table name.
func (cacheMetricBucketProvider) TableName() string {
	return "metric_bucket_providers"
}

type cacheMetricBucketModel struct {
	Granularity      string            `gorm:"primaryKey"`
	Timestamp        time.Time         `gorm:"primaryKey"`
	Bucket           cacheMetricBucket `gorm:"foreignKey:Granularity,Timestamp;references:Granularity,Timestamp;belongsTo;constraint:OnDelete:CASCADE"`
	ModelID          string            `gorm:"primaryKey"`
	Requests         int64             `gorm:"not null"`
	Errors           int64             `gorm:"not null"`
	TokensInput      int64             `gorm:"not null"`
	TokensOutput     int64             `gorm:"not null"`
	PeakRequests     int64             `gorm:"not null"`
	PeakInputTokens  int64             `gorm:"not null"`
	PeakOutputTokens int64             `gorm:"not null"`
}

// TableName pins the metric bucket models table name.
func (cacheMetricBucketModel) TableName() string {
	return "metric_bucket_models"
}

type cacheMetricBucketToken struct {
	Granularity      string            `gorm:"primaryKey"`
	Timestamp        time.Time         `gorm:"primaryKey"`
	Bucket           cacheMetricBucket `gorm:"foreignKey:Granularity,Timestamp;references:Granularity,Timestamp;belongsTo;constraint:OnDelete:CASCADE"`
	TokenID          string            `gorm:"primaryKey"`
	Requests         int64             `gorm:"not null"`
	Errors           int64             `gorm:"not null"`
	TokensInput      int64             `gorm:"not null"`
	TokensOutput     int64             `gorm:"not null"`
	PeakRequests     int64             `gorm:"not null"`
	PeakInputTokens  int64             `gorm:"not null"`
	PeakOutputTokens int64             `gorm:"not null"`
	LastUsed         *time.Time        `gorm:""`
}

// TableName pins the metric bucket tokens table name.
func (cacheMetricBucketToken) TableName() string {
	return "metric_bucket_tokens"
}

type cacheMetricBucketErrorType struct {
	Granularity string            `gorm:"primaryKey"`
	Timestamp   time.Time         `gorm:"primaryKey"`
	Bucket      cacheMetricBucket `gorm:"foreignKey:Granularity,Timestamp;references:Granularity,Timestamp;belongsTo;constraint:OnDelete:CASCADE"`
	ErrorType   string            `gorm:"primaryKey"`
	Count       int64             `gorm:"not null"`
}

// TableName pins the metric bucket error types table name.
func (cacheMetricBucketErrorType) TableName() string {
	return "metric_bucket_error_types"
}

type cacheVideoJob struct {
	ID            string    `gorm:"primaryKey"`
	Model         string    `gorm:"not null"`
	BackendModel  string    `gorm:"not null"`
	ProviderID    string    `gorm:"not null"`
	TypeKey       string    `gorm:"not null"`
	UpstreamJobID string    `gorm:"not null"`
	Status        string    `gorm:"not null;index"`
	CreatedAt     time.Time `gorm:"not null"`
	UpdatedAt     time.Time `gorm:"not null"`
}

// TableName pins the video jobs table name.
func (cacheVideoJob) TableName() string {
	return "video_jobs"
}

type cacheResponseRecord struct {
	ID        string    `gorm:"primaryKey"`
	Kind      string    `gorm:"not null;index"`
	Model     string    `gorm:"not null"`
	Status    string    `gorm:"not null"`
	ParentID  *string   `gorm:"index"`
	RefID     string    `gorm:"not null"`
	Payload   []byte    `gorm:"not null"`
	Input     []byte    `gorm:"not null"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

// TableName pins the response records table name.
func (cacheResponseRecord) TableName() string {
	return "response_records"
}

type cacheMessageBatch struct {
	ID        string     `gorm:"primaryKey"`
	Model     string     `gorm:"not null"`
	Status    string     `gorm:"not null;index"`
	CreatedAt time.Time  `gorm:"not null"`
	UpdatedAt time.Time  `gorm:"not null"`
	EndedAt   *time.Time `gorm:""`
}

// TableName pins the message batches table name.
func (cacheMessageBatch) TableName() string {
	return "message_batches"
}

type cacheMessageBatchEntry struct {
	BatchID  string            `gorm:"primaryKey"`
	Batch    cacheMessageBatch `gorm:"foreignKey:BatchID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Position int               `gorm:"primaryKey"`
	CustomID string            `gorm:"not null"`
	Params   []byte            `gorm:"not null"`
}

// TableName pins the message batch entries table name.
func (cacheMessageBatchEntry) TableName() string {
	return "message_batch_entries"
}

type cacheMessageBatchResult struct {
	BatchID  string            `gorm:"primaryKey"`
	Batch    cacheMessageBatch `gorm:"foreignKey:BatchID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Position int               `gorm:"primaryKey"`
	CustomID string            `gorm:"not null"`
	Result   []byte            `gorm:"not null"`
}

// TableName pins the message batch results table name.
func (cacheMessageBatchResult) TableName() string {
	return "message_batch_results"
}

func upCacheSchema(transaction *gorm.DB) error {
	migrator := transaction.Migrator()
	for _, table := range []any{
		&cacheSession{},
		&cacheProxyState{},
		&cacheModelInfoSnapshot{},
		&cacheModelInfo{},
		&cacheModelInfoCapability{},
		&cacheModelInfoModality{},
		&cacheModelInfoEndpoint{},
		&cacheCredentialPark{},
		&cacheCredentialHealth{},
		&cachePluginStorage{},
		&cacheMetricBucket{},
		&cacheMetricBucketProvider{},
		&cacheMetricBucketModel{},
		&cacheMetricBucketToken{},
		&cacheMetricBucketErrorType{},
		&cacheVideoJob{},
		&cacheResponseRecord{},
		&cacheMessageBatch{},
		&cacheMessageBatchEntry{},
		&cacheMessageBatchResult{},
	} {
		if err := migrator.CreateTable(table); err != nil {
			return fmt.Errorf("create table: %w", err)
		}
	}
	return nil
}

func upSplitStorage(transaction *gorm.DB) error {
	migrator := transaction.Migrator()
	for _, table := range []string{
		"message_batch_entries",
		"message_batch_results",
		"message_batches",
		"metric_bucket_providers",
		"metric_bucket_models",
		"metric_bucket_tokens",
		"metric_bucket_error_types",
		"metric_buckets",
		"model_info_capabilities",
		"model_info_modalities",
		"model_info_endpoints",
		"model_infos",
		"model_info_snapshots",
		"credential_parks",
		"credential_health",
		"proxy_states",
		"sessions",
		"video_jobs",
		"response_records",
	} {
		if err := migrator.DropTable(table); err != nil {
			return fmt.Errorf("drop table %s: %w", table, err)
		}
	}
	if _, err := gorm.G[cachePluginStorage](transaction, clause.Neq{Column: "expires_at", Value: nil}).Delete(context.Background()); err != nil {
		return fmt.Errorf("delete ttl plugin storage rows: %w", err)
	}
	return nil
}

// LegacySingleFile reports whether the primary database still holds the
// single-file layout. The sessions table only ever lived in the primary
// file before the split, so its presence marks a database that predates
// version 2. Fresh and already-split files never carry it.
func LegacySingleFile(primary *gorm.DB) bool {
	if primary == nil {
		return false
	}
	return primary.Migrator().HasTable("sessions")
}

// CopyLegacyCacheTables copies every moved table from a single-file primary
// database into the cache database. Inserts upsert so a repeated run after
// an interrupted startup converges instead of duplicating. Only plugin
// storage rows with a TTL move; durable rows stay in the primary file.
func CopyLegacyCacheTables(ctx context.Context, primary, cache *gorm.DB) error {
	if err := copyCacheTable[cacheSession](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheProxyState](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheModelInfoSnapshot](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheModelInfo](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheModelInfoCapability](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheModelInfoModality](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheModelInfoEndpoint](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheCredentialPark](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheCredentialHealth](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTableWhere[cachePluginStorage](ctx, primary, cache, clause.Neq{Column: "expires_at", Value: nil}); err != nil {
		return err
	}
	if err := copyCacheTable[cacheMetricBucket](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheMetricBucketProvider](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheMetricBucketModel](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheMetricBucketToken](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheMetricBucketErrorType](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheVideoJob](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheResponseRecord](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheMessageBatch](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheMessageBatchEntry](ctx, primary, cache); err != nil {
		return err
	}
	if err := copyCacheTable[cacheMessageBatchResult](ctx, primary, cache); err != nil {
		return err
	}
	return nil
}

func copyCacheTable[Row any](ctx context.Context, primary, cache *gorm.DB) error {
	return copyCacheTableWhere[Row](ctx, primary, cache)
}

// copyCacheTableWhere streams one table from primary to cache in batches so
// large telemetry tables never load fully into memory. The cache upsert
// carries UpdateAll so interrupted startups converge on retry.
func copyCacheTableWhere[Row any](ctx context.Context, primary, cache *gorm.DB, conditions ...clause.Expression) error {
	var batch []Row
	query := primary.WithContext(ctx)
	if len(conditions) > 0 {
		query = query.Clauses(clause.Where{Exprs: conditions})
	}
	if err := query.FindInBatches(&batch, 500, func(_ *gorm.DB, _ int) error {
		if len(batch) == 0 {
			return nil
		}
		rows := batch
		if err := gorm.G[Row](cache, clause.OnConflict{UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
			return fmt.Errorf("copy cache table: %w", err)
		}
		return nil
	}).Error; err != nil {
		return fmt.Errorf("read legacy cache table: %w", err)
	}
	return nil
}
