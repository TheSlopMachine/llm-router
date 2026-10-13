// Package migrations owns numbered SQLite schema changes.
// Each migration declares its own snapshot structs and never imports
// internal/db/record so later model changes cannot rewrite history.
package migrations

import (
	"fmt"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/migrate"
	"gorm.io/gorm"
)

// RegistryDatabase lists every migration for the durable database file in
// order. Version numbers are never reused.
var RegistryDatabase = []migrate.Migration{
	{Version: 1, Name: "initial_schema", Up: upInitialSchema},
	{Version: 2, Name: "split_storage", Up: upSplitStorage},
	{Version: 3, Name: "plugin_disabled", Up: upPluginDisabled},
}

type instanceSettingSnapshot struct {
	ID               int64     `gorm:"primaryKey;check:id = 1"`
	Bootstrapped     bool      `gorm:"not null"`
	IsClusterNode    bool      `gorm:"not null"`
	DisableTelemetry bool      `gorm:"not null"`
	ModelsFilter     string    `gorm:"not null;check:models_filter IN ('all','enabled','disabled')"`
	UpdatedAt        time.Time `gorm:"not null"`
}

// TableName pins the instance settings table name.
func (instanceSettingSnapshot) TableName() string {
	return "instance_settings"
}

type adminUserSnapshot struct {
	Username     string    `gorm:"primaryKey"`
	PasswordHash string    `gorm:"not null"`
	CreatedAt    time.Time `gorm:"not null"`
}

// TableName pins the admin users table name.
func (adminUserSnapshot) TableName() string {
	return "admin_users"
}

type sessionSnapshot struct {
	TokenHash  string            `gorm:"primaryKey"`
	Username   string            `gorm:"not null;index"`
	AdminUser  adminUserSnapshot `gorm:"foreignKey:Username;references:Username;belongsTo;constraint:OnDelete:CASCADE"`
	CreatedAt  time.Time         `gorm:"not null"`
	ExpiresAt  time.Time         `gorm:"not null;index"`
	RememberMe bool              `gorm:"not null"`
}

// TableName pins the sessions table name.
func (sessionSnapshot) TableName() string {
	return "sessions"
}

type proxyPoolSnapshot struct {
	ID        string    `gorm:"primaryKey"`
	Name      string    `gorm:"not null"`
	Builtin   bool      `gorm:"not null"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

// TableName pins the proxy pools table name.
func (proxyPoolSnapshot) TableName() string {
	return "proxy_pools"
}

type proxyPoolEntrySnapshot struct {
	PoolID   string            `gorm:"primaryKey"`
	Pool     proxyPoolSnapshot `gorm:"foreignKey:PoolID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Position int               `gorm:"primaryKey"`
	URL      string            `gorm:"not null"`
	Country  string            `gorm:"not null"`
}

// TableName pins the proxy pool entries table name.
func (proxyPoolEntrySnapshot) TableName() string {
	return "proxy_pool_entries"
}

type proxyStateSnapshot struct {
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
func (proxyStateSnapshot) TableName() string {
	return "proxy_states"
}

type routerTokenSnapshot struct {
	ID                  string    `gorm:"primaryKey"`
	Name                string    `gorm:"not null"`
	TokenHash           string    `gorm:"not null;uniqueIndex"`
	AllowAllProviders   bool      `gorm:"not null"`
	AllowAllModels      bool      `gorm:"not null"`
	AllowAllCredentials bool      `gorm:"not null"`
	CreatedAt           time.Time `gorm:"not null"`
}

// TableName pins the router tokens table name.
func (routerTokenSnapshot) TableName() string {
	return "router_tokens"
}

type providerInstanceSnapshot struct {
	ID                       string             `gorm:"primaryKey"`
	Name                     string             `gorm:"not null"`
	TypeKey                  string             `gorm:"not null;index"`
	BaseURL                  *string            `gorm:""`
	ProxyPoolID              *string            `gorm:""`
	ProxyPool                *proxyPoolSnapshot `gorm:"foreignKey:ProxyPoolID;references:ID;belongsTo;constraint:OnDelete:RESTRICT"`
	DisableFailedCredentials bool               `gorm:"not null"`
	Disabled                 bool               `gorm:"not null"`
	DisabledBy               string             `gorm:"not null"`
	DisabledReason           string             `gorm:"not null"`
	DisabledAt               *time.Time         `gorm:""`
	IsUIReadonly             bool               `gorm:"not null"`
	IsUIHidden               bool               `gorm:"not null"`
	IconURL                  string             `gorm:"not null"`
	Settings                 map[string]any     `gorm:"serializer:json"`
	CreatedAt                time.Time          `gorm:"not null"`
	UpdatedAt                time.Time          `gorm:"not null"`
}

// TableName pins the provider instances table name.
func (providerInstanceSnapshot) TableName() string {
	return "provider_instances"
}

type tokenAllowedProviderSnapshot struct {
	TokenID    string                   `gorm:"primaryKey"`
	Token      routerTokenSnapshot      `gorm:"foreignKey:TokenID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	ProviderID string                   `gorm:"primaryKey"`
	Provider   providerInstanceSnapshot `gorm:"foreignKey:ProviderID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
}

// TableName pins the token allowed providers table name.
func (tokenAllowedProviderSnapshot) TableName() string {
	return "token_allowed_providers"
}

type tokenAllowedModelSnapshot struct {
	TokenID string              `gorm:"primaryKey"`
	Token   routerTokenSnapshot `gorm:"foreignKey:TokenID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	ModelID string              `gorm:"primaryKey"`
}

// TableName pins the token allowed models table name.
func (tokenAllowedModelSnapshot) TableName() string {
	return "token_allowed_models"
}

type credentialSnapshot struct {
	ID             string                   `gorm:"primaryKey"`
	ProviderID     string                   `gorm:"not null;index"`
	Provider       providerInstanceSnapshot `gorm:"foreignKey:ProviderID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Label          string                   `gorm:"not null"`
	Data           map[string]any           `gorm:"serializer:json"`
	ExpiresAt      *time.Time               `gorm:"index"`
	LastUsedAt     *time.Time               `gorm:""`
	RequestCount   int64                    `gorm:"not null"`
	SuccessCount   int64                    `gorm:"not null"`
	FailureCount   int64                    `gorm:"not null"`
	Disabled       bool                     `gorm:"not null"`
	DisabledBy     string                   `gorm:"not null"`
	DisabledReason string                   `gorm:"not null"`
	DisabledAt     *time.Time               `gorm:""`
	SortOrder      int                      `gorm:"not null"`
	CreatedAt      time.Time                `gorm:"not null"`
	UpdatedAt      time.Time                `gorm:"not null"`
}

// TableName pins the credentials table name.
func (credentialSnapshot) TableName() string {
	return "credentials"
}

type tokenAllowedCredentialSnapshot struct {
	TokenID      string              `gorm:"primaryKey"`
	Token        routerTokenSnapshot `gorm:"foreignKey:TokenID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	CredentialID string              `gorm:"primaryKey"`
	Credential   credentialSnapshot  `gorm:"foreignKey:CredentialID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
}

// TableName pins the token allowed credentials table name.
func (tokenAllowedCredentialSnapshot) TableName() string {
	return "token_allowed_credentials"
}

type tokenCredentialScopeSnapshot struct {
	ID             string                   `gorm:"primaryKey"`
	TokenID        string                   `gorm:"not null;uniqueIndex:idx_token_scopes_provider"`
	Token          routerTokenSnapshot      `gorm:"foreignKey:TokenID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	ProviderID     string                   `gorm:"not null;uniqueIndex:idx_token_scopes_provider"`
	Provider       providerInstanceSnapshot `gorm:"foreignKey:ProviderID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	AllCredentials bool                     `gorm:"not null"`
}

// TableName pins the token credential scopes table name.
func (tokenCredentialScopeSnapshot) TableName() string {
	return "token_credential_scopes"
}

type tokenScopeCredentialSnapshot struct {
	ScopeID      string                       `gorm:"primaryKey"`
	Scope        tokenCredentialScopeSnapshot `gorm:"foreignKey:ScopeID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	CredentialID string                       `gorm:"primaryKey"`
	Credential   credentialSnapshot           `gorm:"foreignKey:CredentialID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
}

// TableName pins the token scope credentials table name.
func (tokenScopeCredentialSnapshot) TableName() string {
	return "token_scope_credentials"
}

type credentialParkSnapshot struct {
	CredentialID string             `gorm:"primaryKey"`
	Credential   credentialSnapshot `gorm:"foreignKey:CredentialID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Reason       string             `gorm:"not null"`
	Until        time.Time          `gorm:"not null;index"`
	ParkedAt     time.Time          `gorm:"not null"`
}

// TableName pins the credential parks table name.
func (credentialParkSnapshot) TableName() string {
	return "credential_parks"
}

type credentialHealthSnapshot struct {
	CredentialID string             `gorm:"primaryKey"`
	Credential   credentialSnapshot `gorm:"foreignKey:CredentialID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	LastCheckAt  time.Time          `gorm:"not null"`
	LastStatus   string             `gorm:"not null"`
}

// TableName pins the credential health table name.
func (credentialHealthSnapshot) TableName() string {
	return "credential_health"
}

type modelOverrideSnapshot struct {
	ProviderID  string                   `gorm:"primaryKey"`
	Provider    providerInstanceSnapshot `gorm:"foreignKey:ProviderID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	ModelName   string                   `gorm:"primaryKey"`
	Disabled    bool                     `gorm:"not null"`
	Custom      bool                     `gorm:"not null"`
	DisplayName string                   `gorm:"not null"`
}

// TableName pins the model overrides table name.
func (modelOverrideSnapshot) TableName() string {
	return "model_overrides"
}

type modelOverrideCapabilitySnapshot struct {
	ProviderID string                `gorm:"primaryKey"`
	ModelName  string                `gorm:"primaryKey"`
	Override   modelOverrideSnapshot `gorm:"foreignKey:ProviderID,ModelName;references:ProviderID,ModelName;belongsTo;constraint:OnDelete:CASCADE"`
	Capability string                `gorm:"primaryKey"`
}

// TableName pins the model override capabilities table name.
func (modelOverrideCapabilitySnapshot) TableName() string {
	return "model_override_capabilities"
}

type modelOverrideModalitySnapshot struct {
	ProviderID string                `gorm:"primaryKey"`
	ModelName  string                `gorm:"primaryKey"`
	Override   modelOverrideSnapshot `gorm:"foreignKey:ProviderID,ModelName;references:ProviderID,ModelName;belongsTo;constraint:OnDelete:CASCADE"`
	Direction  string                `gorm:"primaryKey;check:direction IN ('input','output')"`
	Modality   string                `gorm:"primaryKey"`
}

// TableName pins the model override modalities table name.
func (modelOverrideModalitySnapshot) TableName() string {
	return "model_override_modalities"
}

type modelInfoSnapshotSnapshot struct {
	ProviderID string                   `gorm:"primaryKey"`
	Provider   providerInstanceSnapshot `gorm:"foreignKey:ProviderID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	CachedAt   time.Time                `gorm:"not null"`
}

// TableName pins the model info snapshots table name.
func (modelInfoSnapshotSnapshot) TableName() string {
	return "model_info_snapshots"
}

type modelInfoSnapshot struct {
	ProviderID          string                    `gorm:"primaryKey"`
	Snapshot            modelInfoSnapshotSnapshot `gorm:"foreignKey:ProviderID;references:ProviderID;belongsTo;constraint:OnDelete:CASCADE"`
	ModelName           string                    `gorm:"primaryKey"`
	DisplayName         string                    `gorm:"not null"`
	Description         string                    `gorm:"not null"`
	RequestsPerMinute   int64                     `gorm:"not null"`
	TokensPerMinute     int64                     `gorm:"not null"`
	RequestsPerDay      int64                     `gorm:"not null"`
	ContextWindow       int64                     `gorm:"not null"`
	MaxTokens           int64                     `gorm:"not null"`
	SupportedParameters []string                  `gorm:"serializer:json"`
	Reasoning           []byte                    `gorm:""`
}

// TableName pins the model infos table name.
func (modelInfoSnapshot) TableName() string {
	return "model_infos"
}

type modelInfoCapabilitySnapshot struct {
	ProviderID string            `gorm:"primaryKey"`
	ModelName  string            `gorm:"primaryKey"`
	Model      modelInfoSnapshot `gorm:"foreignKey:ProviderID,ModelName;references:ProviderID,ModelName;belongsTo;constraint:OnDelete:CASCADE"`
	Capability string            `gorm:"primaryKey"`
}

// TableName pins the model info capabilities table name.
func (modelInfoCapabilitySnapshot) TableName() string {
	return "model_info_capabilities"
}

type modelInfoModalitySnapshot struct {
	ProviderID string            `gorm:"primaryKey"`
	ModelName  string            `gorm:"primaryKey"`
	Model      modelInfoSnapshot `gorm:"foreignKey:ProviderID,ModelName;references:ProviderID,ModelName;belongsTo;constraint:OnDelete:CASCADE"`
	Direction  string            `gorm:"primaryKey;check:direction IN ('input','output')"`
	Modality   string            `gorm:"primaryKey"`
}

// TableName pins the model info modalities table name.
func (modelInfoModalitySnapshot) TableName() string {
	return "model_info_modalities"
}

type modelInfoEndpointSnapshot struct {
	ProviderID string            `gorm:"primaryKey"`
	ModelName  string            `gorm:"primaryKey"`
	Model      modelInfoSnapshot `gorm:"foreignKey:ProviderID,ModelName;references:ProviderID,ModelName;belongsTo;constraint:OnDelete:CASCADE"`
	Endpoint   string            `gorm:"primaryKey"`
}

// TableName pins the model info endpoints table name.
func (modelInfoEndpointSnapshot) TableName() string {
	return "model_info_endpoints"
}

type virtualModelSnapshot struct {
	ID                  string                    `gorm:"primaryKey"`
	Name                string                    `gorm:"not null"`
	NameKey             string                    `gorm:"not null;uniqueIndex"`
	Description         string                    `gorm:"not null"`
	Instruction         string                    `gorm:"not null"`
	ManagedProviderID   *string                   `gorm:""`
	ManagedProvider     *providerInstanceSnapshot `gorm:"foreignKey:ManagedProviderID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	ManagedEndpointSlug *string                   `gorm:""`
	Disabled            bool                      `gorm:"not null"`
	Version             int                       `gorm:"not null"`
	CreatedAt           time.Time                 `gorm:"not null"`
	UpdatedAt           time.Time                 `gorm:"not null"`
}

// TableName pins the virtual models table name.
func (virtualModelSnapshot) TableName() string {
	return "virtual_models"
}

type virtualModelMemberSnapshot struct {
	VirtualModelID string                   `gorm:"primaryKey"`
	VirtualModel   virtualModelSnapshot     `gorm:"foreignKey:VirtualModelID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Position       int                      `gorm:"primaryKey"`
	ProviderID     string                   `gorm:"not null"`
	Provider       providerInstanceSnapshot `gorm:"foreignKey:ProviderID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	ModelName      string                   `gorm:"not null"`
}

// TableName pins the virtual model members table name.
func (virtualModelMemberSnapshot) TableName() string {
	return "virtual_model_members"
}

type pluginRepositorySnapshot struct {
	ID          string    `gorm:"primaryKey"`
	Kind        string    `gorm:"not null"`
	IndexURL    string    `gorm:"not null;uniqueIndex"`
	SourceURL   string    `gorm:"not null"`
	Title       string    `gorm:"not null"`
	Description string    `gorm:"not null"`
	Builtin     bool      `gorm:"not null"`
	AddedAt     time.Time `gorm:"not null"`
}

// TableName pins the plugin repositories table name.
func (pluginRepositorySnapshot) TableName() string {
	return "plugin_repositories"
}

type pluginSnapshot struct {
	ID                 string                    `gorm:"primaryKey"`
	DisplayName        string                    `gorm:"not null"`
	Author             string                    `gorm:"not null"`
	Version            string                    `gorm:"not null"`
	PluginAPIVersion   string                    `gorm:"not null"`
	Description        string                    `gorm:"not null"`
	License            string                    `gorm:"not null"`
	Unsafe             bool                      `gorm:"not null"`
	OriginRepositoryID *string                   `gorm:""`
	OriginRepository   *pluginRepositorySnapshot `gorm:"foreignKey:OriginRepositoryID;references:ID;belongsTo;constraint:OnDelete:SET NULL"`
	OriginPath         string                    `gorm:"not null"`
	OriginManual       bool                      `gorm:"not null"`
	Declarations       map[string]any            `gorm:"serializer:json"`
	Source             []byte                    `gorm:"not null"`
	InstalledAt        time.Time                 `gorm:"not null"`
	UpdatedAt          time.Time                 `gorm:"not null"`
}

// TableName pins the plugins table name.
func (pluginSnapshot) TableName() string {
	return "plugins"
}

type pluginAllowHostSnapshot struct {
	PluginID string         `gorm:"primaryKey"`
	Plugin   pluginSnapshot `gorm:"foreignKey:PluginID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Host     string         `gorm:"primaryKey"`
}

// TableName pins the plugin allow hosts table name.
func (pluginAllowHostSnapshot) TableName() string {
	return "plugin_allow_hosts"
}

type pluginVersionSnapshot struct {
	ID           string         `gorm:"primaryKey"`
	PluginID     string         `gorm:"not null;index"`
	Plugin       pluginSnapshot `gorm:"foreignKey:PluginID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Version      string         `gorm:"not null"`
	Source       []byte         `gorm:"not null"`
	Declarations map[string]any `gorm:"serializer:json"`
	CreatedAt    time.Time      `gorm:"not null"`
}

// TableName pins the plugin versions table name.
func (pluginVersionSnapshot) TableName() string {
	return "plugin_versions"
}

type pluginStorageSnapshot struct {
	PluginID  string         `gorm:"primaryKey"`
	Plugin    pluginSnapshot `gorm:"foreignKey:PluginID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Scope     string         `gorm:"primaryKey"`
	Key       string         `gorm:"primaryKey"`
	Value     []byte         `gorm:"not null"`
	ExpiresAt *time.Time     `gorm:"index"`
}

// TableName pins the plugin storage table name.
func (pluginStorageSnapshot) TableName() string {
	return "plugin_storage"
}

type videoJobSnapshot struct {
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
func (videoJobSnapshot) TableName() string {
	return "video_jobs"
}

type responseRecordSnapshot struct {
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
func (responseRecordSnapshot) TableName() string {
	return "response_records"
}

type messageBatchSnapshot struct {
	ID        string     `gorm:"primaryKey"`
	Model     string     `gorm:"not null"`
	Status    string     `gorm:"not null;index"`
	CreatedAt time.Time  `gorm:"not null"`
	UpdatedAt time.Time  `gorm:"not null"`
	EndedAt   *time.Time `gorm:""`
}

// TableName pins the message batches table name.
func (messageBatchSnapshot) TableName() string {
	return "message_batches"
}

type messageBatchEntrySnapshot struct {
	BatchID  string               `gorm:"primaryKey"`
	Batch    messageBatchSnapshot `gorm:"foreignKey:BatchID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Position int                  `gorm:"primaryKey"`
	CustomID string               `gorm:"not null"`
	Params   []byte               `gorm:"not null"`
}

// TableName pins the message batch entries table name.
func (messageBatchEntrySnapshot) TableName() string {
	return "message_batch_entries"
}

type messageBatchResultSnapshot struct {
	BatchID  string               `gorm:"primaryKey"`
	Batch    messageBatchSnapshot `gorm:"foreignKey:BatchID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Position int                  `gorm:"primaryKey"`
	CustomID string               `gorm:"not null"`
	Result   []byte               `gorm:"not null"`
}

// TableName pins the message batch results table name.
func (messageBatchResultSnapshot) TableName() string {
	return "message_batch_results"
}

type metricBucketSnapshot struct {
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
func (metricBucketSnapshot) TableName() string {
	return "metric_buckets"
}

type metricBucketProviderSnapshot struct {
	Granularity      string               `gorm:"primaryKey"`
	Timestamp        time.Time            `gorm:"primaryKey"`
	Bucket           metricBucketSnapshot `gorm:"foreignKey:Granularity,Timestamp;references:Granularity,Timestamp;belongsTo;constraint:OnDelete:CASCADE"`
	Dimension        string               `gorm:"primaryKey;check:dimension IN ('provider_id','provider_type')"`
	Key              string               `gorm:"primaryKey"`
	Requests         int64                `gorm:"not null"`
	Errors           int64                `gorm:"not null"`
	TokensInput      int64                `gorm:"not null"`
	TokensOutput     int64                `gorm:"not null"`
	PeakRequests     int64                `gorm:"not null"`
	PeakInputTokens  int64                `gorm:"not null"`
	PeakOutputTokens int64                `gorm:"not null"`
}

// TableName pins the metric bucket providers table name.
func (metricBucketProviderSnapshot) TableName() string {
	return "metric_bucket_providers"
}

type metricBucketModelSnapshot struct {
	Granularity      string               `gorm:"primaryKey"`
	Timestamp        time.Time            `gorm:"primaryKey"`
	Bucket           metricBucketSnapshot `gorm:"foreignKey:Granularity,Timestamp;references:Granularity,Timestamp;belongsTo;constraint:OnDelete:CASCADE"`
	ModelID          string               `gorm:"primaryKey"`
	Requests         int64                `gorm:"not null"`
	Errors           int64                `gorm:"not null"`
	TokensInput      int64                `gorm:"not null"`
	TokensOutput     int64                `gorm:"not null"`
	PeakRequests     int64                `gorm:"not null"`
	PeakInputTokens  int64                `gorm:"not null"`
	PeakOutputTokens int64                `gorm:"not null"`
}

// TableName pins the metric bucket models table name.
func (metricBucketModelSnapshot) TableName() string {
	return "metric_bucket_models"
}

type metricBucketTokenSnapshot struct {
	Granularity      string               `gorm:"primaryKey"`
	Timestamp        time.Time            `gorm:"primaryKey"`
	Bucket           metricBucketSnapshot `gorm:"foreignKey:Granularity,Timestamp;references:Granularity,Timestamp;belongsTo;constraint:OnDelete:CASCADE"`
	TokenID          string               `gorm:"primaryKey"`
	Requests         int64                `gorm:"not null"`
	Errors           int64                `gorm:"not null"`
	TokensInput      int64                `gorm:"not null"`
	TokensOutput     int64                `gorm:"not null"`
	PeakRequests     int64                `gorm:"not null"`
	PeakInputTokens  int64                `gorm:"not null"`
	PeakOutputTokens int64                `gorm:"not null"`
	LastUsed         *time.Time           `gorm:""`
}

// TableName pins the metric bucket tokens table name.
func (metricBucketTokenSnapshot) TableName() string {
	return "metric_bucket_tokens"
}

type metricBucketErrorTypeSnapshot struct {
	Granularity string               `gorm:"primaryKey"`
	Timestamp   time.Time            `gorm:"primaryKey"`
	Bucket      metricBucketSnapshot `gorm:"foreignKey:Granularity,Timestamp;references:Granularity,Timestamp;belongsTo;constraint:OnDelete:CASCADE"`
	ErrorType   string               `gorm:"primaryKey"`
	Count       int64                `gorm:"not null"`
}

// TableName pins the metric bucket error types table name.
func (metricBucketErrorTypeSnapshot) TableName() string {
	return "metric_bucket_error_types"
}

func upInitialSchema(transaction *gorm.DB) error {
	migrator := transaction.Migrator()
	for _, table := range []any{
		&instanceSettingSnapshot{},
		&adminUserSnapshot{},
		&proxyPoolSnapshot{},
		&proxyStateSnapshot{},
		&routerTokenSnapshot{},
		&providerInstanceSnapshot{},
		&pluginRepositorySnapshot{},
		&sessionSnapshot{},
		&proxyPoolEntrySnapshot{},
		&tokenAllowedProviderSnapshot{},
		&tokenAllowedModelSnapshot{},
		&credentialSnapshot{},
		&tokenAllowedCredentialSnapshot{},
		&tokenCredentialScopeSnapshot{},
		&tokenScopeCredentialSnapshot{},
		&credentialParkSnapshot{},
		&credentialHealthSnapshot{},
		&modelOverrideSnapshot{},
		&modelOverrideCapabilitySnapshot{},
		&modelOverrideModalitySnapshot{},
		&modelInfoSnapshotSnapshot{},
		&modelInfoSnapshot{},
		&modelInfoCapabilitySnapshot{},
		&modelInfoModalitySnapshot{},
		&modelInfoEndpointSnapshot{},
		&virtualModelSnapshot{},
		&virtualModelMemberSnapshot{},
		&pluginSnapshot{},
		&pluginAllowHostSnapshot{},
		&pluginVersionSnapshot{},
		&pluginStorageSnapshot{},
		&videoJobSnapshot{},
		&responseRecordSnapshot{},
		&messageBatchSnapshot{},
		&messageBatchEntrySnapshot{},
		&messageBatchResultSnapshot{},
		&metricBucketSnapshot{},
		&metricBucketProviderSnapshot{},
		&metricBucketModelSnapshot{},
		&metricBucketTokenSnapshot{},
		&metricBucketErrorTypeSnapshot{},
	} {
		if err := migrator.CreateTable(table); err != nil {
			return fmt.Errorf("create table: %w", err)
		}
	}
	return nil
}
