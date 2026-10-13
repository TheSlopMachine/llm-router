package record

import (
	"time"
)

// PluginRepository is one added plugin store: a single-URL index repo.
// Built-in repos seed on startup and reject removal in the service.
type PluginRepository struct {
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
func (PluginRepository) TableName() string {
	return "plugin_repositories"
}

// Plugin is one installed provider backend. Declarations stay JSON because
// the shape is plugin-owned: type keys, handlers, icons, recursive UI
// schema trees, jobs, model specifications, health cooldowns, proxy source
// keys. Source lands in a BLOB column, without base64 overhead.
type Plugin struct {
	ID                 string            `gorm:"primaryKey"`
	DisplayName        string            `gorm:"not null"`
	Author             string            `gorm:"not null"`
	Version            string            `gorm:"not null"`
	PluginAPIVersion   string            `gorm:"not null"`
	Description        string            `gorm:"not null"`
	License            string            `gorm:"not null"`
	Unsafe             bool              `gorm:"not null"`
	Disabled           bool              `gorm:"not null"`
	OriginRepositoryID *string           `gorm:""`
	OriginRepository   *PluginRepository `gorm:"foreignKey:OriginRepositoryID;references:ID;belongsTo;constraint:OnDelete:SET NULL"`
	OriginPath         string            `gorm:"not null"`
	OriginManual       bool              `gorm:"not null"`
	Declarations       map[string]any    `gorm:"serializer:json"`
	Source             []byte            `gorm:"not null"`
	InstalledAt        time.Time         `gorm:"not null"`
	UpdatedAt          time.Time         `gorm:"not null"`
}

// TableName pins the plugins table name.
func (Plugin) TableName() string {
	return "plugins"
}

// PluginAllowHost is one router-enforced allow-list entry.
type PluginAllowHost struct {
	PluginID string `gorm:"primaryKey"`
	Plugin   Plugin `gorm:"foreignKey:PluginID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Host     string `gorm:"primaryKey"`
}

// TableName pins the plugin allow hosts table name.
func (PluginAllowHost) TableName() string {
	return "plugin_allow_hosts"
}

// PluginVersion is one rollback history entry. Retention follows the
// luaplugin cap: ten full-source snapshots per plugin.
type PluginVersion struct {
	ID           string         `gorm:"primaryKey"`
	PluginID     string         `gorm:"not null;index"`
	Plugin       Plugin         `gorm:"foreignKey:PluginID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Version      string         `gorm:"not null"`
	Source       []byte         `gorm:"not null"`
	Declarations map[string]any `gorm:"serializer:json"`
	CreatedAt    time.Time      `gorm:"not null"`
}

// TableName pins the plugin versions table name.
func (PluginVersion) TableName() string {
	return "plugin_versions"
}

// PluginStorage is one llm_router.storage entry. The composite primary key
// replaces the NUL-separated bucket string. Value stays JSON because the
// shape is plugin-owned; expiry is an indexed column.
type PluginStorage struct {
	PluginID  string     `gorm:"primaryKey"`
	Plugin    Plugin     `gorm:"foreignKey:PluginID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Scope     string     `gorm:"primaryKey"`
	Key       string     `gorm:"primaryKey"`
	Value     []byte     `gorm:"not null"`
	ExpiresAt *time.Time `gorm:"index"`
}

// TableName pins the plugin storage table name.
func (PluginStorage) TableName() string {
	return "plugin_storage"
}
