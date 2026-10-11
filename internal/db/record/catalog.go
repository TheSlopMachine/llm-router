package record

import (
	"time"
)

// ModelOverride is the per-provider admin override for one model: disabled
// from routing, registered as custom, or display-adjusted. Lists normalize
// into child rows because the router filters on them.
type ModelOverride struct {
	ProviderID  string           `gorm:"primaryKey"`
	Provider    ProviderInstance `gorm:"foreignKey:ProviderID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	ModelName   string           `gorm:"primaryKey"`
	Disabled    bool             `gorm:"not null"`
	Custom      bool             `gorm:"not null"`
	DisplayName string           `gorm:"not null"`
}

// TableName pins the model overrides table name.
func (ModelOverride) TableName() string {
	return "model_overrides"
}

// ModelOverrideCapability is one capability chip on an override.
type ModelOverrideCapability struct {
	ProviderID string        `gorm:"primaryKey"`
	ModelName  string        `gorm:"primaryKey"`
	Override   ModelOverride `gorm:"foreignKey:ProviderID,ModelName;references:ProviderID,ModelName;belongsTo;constraint:OnDelete:CASCADE"`
	Capability string        `gorm:"primaryKey"`
}

// TableName pins the model override capabilities table name.
func (ModelOverrideCapability) TableName() string {
	return "model_override_capabilities"
}

// ModelOverrideModality is one verified modality on an override.
type ModelOverrideModality struct {
	ProviderID string        `gorm:"primaryKey"`
	ModelName  string        `gorm:"primaryKey"`
	Override   ModelOverride `gorm:"foreignKey:ProviderID,ModelName;references:ProviderID,ModelName;belongsTo;constraint:OnDelete:CASCADE"`
	Direction  string        `gorm:"primaryKey;check:direction IN ('input','output')"`
	Modality   string        `gorm:"primaryKey"`
}

// TableName pins the model override modalities table name.
func (ModelOverrideModality) TableName() string {
	return "model_override_modalities"
}

// ModelInfoSnapshot is one provider's model metadata cache header.
type ModelInfoSnapshot struct {
	ProviderID string           `gorm:"primaryKey"`
	Provider   ProviderInstance `gorm:"foreignKey:ProviderID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	CachedAt   time.Time        `gorm:"not null"`
}

// TableName pins the model info snapshots table name.
func (ModelInfoSnapshot) TableName() string {
	return "model_info_snapshots"
}

// ModelInfo is one cached upstream model row. SupportedParameters and
// Reasoning stay JSON because the vocabulary is upstream-defined.
type ModelInfo struct {
	ProviderID          string            `gorm:"primaryKey"`
	Snapshot            ModelInfoSnapshot `gorm:"foreignKey:ProviderID;references:ProviderID;belongsTo;constraint:OnDelete:CASCADE"`
	ModelName           string            `gorm:"primaryKey"`
	DisplayName         string            `gorm:"not null"`
	Description         string            `gorm:"not null"`
	RequestsPerMinute   int64             `gorm:"not null"`
	TokensPerMinute     int64             `gorm:"not null"`
	RequestsPerDay      int64             `gorm:"not null"`
	ContextWindow       int64             `gorm:"not null"`
	MaxTokens           int64             `gorm:"not null"`
	SupportedParameters []string          `gorm:"serializer:json"`
	Reasoning           []byte            `gorm:""`
}

// TableName pins the model infos table name.
func (ModelInfo) TableName() string {
	return "model_infos"
}

// ModelInfoCapability is one capability chip on a cached model.
type ModelInfoCapability struct {
	ProviderID string    `gorm:"primaryKey"`
	ModelName  string    `gorm:"primaryKey"`
	Model      ModelInfo `gorm:"foreignKey:ProviderID,ModelName;references:ProviderID,ModelName;belongsTo;constraint:OnDelete:CASCADE"`
	Capability string    `gorm:"primaryKey"`
}

// TableName pins the model info capabilities table name.
func (ModelInfoCapability) TableName() string {
	return "model_info_capabilities"
}

// ModelInfoModality is one modality on a cached model.
type ModelInfoModality struct {
	ProviderID string    `gorm:"primaryKey"`
	ModelName  string    `gorm:"primaryKey"`
	Model      ModelInfo `gorm:"foreignKey:ProviderID,ModelName;references:ProviderID,ModelName;belongsTo;constraint:OnDelete:CASCADE"`
	Direction  string    `gorm:"primaryKey;check:direction IN ('input','output')"`
	Modality   string    `gorm:"primaryKey"`
}

// TableName pins the model info modalities table name.
func (ModelInfoModality) TableName() string {
	return "model_info_modalities"
}

// ModelInfoEndpoint is one served endpoint on a cached model.
type ModelInfoEndpoint struct {
	ProviderID string    `gorm:"primaryKey"`
	ModelName  string    `gorm:"primaryKey"`
	Model      ModelInfo `gorm:"foreignKey:ProviderID,ModelName;references:ProviderID,ModelName;belongsTo;constraint:OnDelete:CASCADE"`
	Endpoint   string    `gorm:"primaryKey"`
}

// TableName pins the model info endpoints table name.
func (ModelInfoEndpoint) TableName() string {
	return "model_info_endpoints"
}
