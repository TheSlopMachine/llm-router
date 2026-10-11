package record

import (
	"time"
)

// ProviderInstance is the single persisted provider row for every type:
// lua-plugin type keys plus built-in custom and virtual.
type ProviderInstance struct {
	ID                       string         `gorm:"primaryKey"`
	Name                     string         `gorm:"not null"`
	TypeKey                  string         `gorm:"not null;index"`
	BaseURL                  *string        `gorm:""`
	ProxyPoolID              *string        `gorm:""`
	ProxyPool                *ProxyPool     `gorm:"foreignKey:ProxyPoolID;references:ID;belongsTo;constraint:OnDelete:RESTRICT"`
	DisableFailedCredentials bool           `gorm:"not null"`
	Disabled                 bool           `gorm:"not null"`
	DisabledBy               string         `gorm:"not null"`
	DisabledReason           string         `gorm:"not null"`
	DisabledAt               *time.Time     `gorm:""`
	IsUIReadonly             bool           `gorm:"not null"`
	IsUIHidden               bool           `gorm:"not null"`
	IconURL                  string         `gorm:"not null"`
	Settings                 map[string]any `gorm:"serializer:json"`
	CreatedAt                time.Time      `gorm:"not null"`
	UpdatedAt                time.Time      `gorm:"not null"`
}

// TableName pins the provider instances table name.
func (ProviderInstance) TableName() string {
	return "provider_instances"
}

// Credential holds one provider authentication row. Data stays JSON because
// the shape is plugin-owned through the credential schema.
type Credential struct {
	ID             string           `gorm:"primaryKey"`
	ProviderID     string           `gorm:"not null;index"`
	Provider       ProviderInstance `gorm:"foreignKey:ProviderID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Label          string           `gorm:"not null"`
	Data           map[string]any   `gorm:"serializer:json"`
	ExpiresAt      *time.Time       `gorm:"index"`
	LastUsedAt     *time.Time       `gorm:""`
	RequestCount   int64            `gorm:"not null"`
	SuccessCount   int64            `gorm:"not null"`
	FailureCount   int64            `gorm:"not null"`
	Disabled       bool             `gorm:"not null"`
	DisabledBy     string           `gorm:"not null"`
	DisabledReason string           `gorm:"not null"`
	DisabledAt     *time.Time       `gorm:""`
	SortOrder      int              `gorm:"not null"`
	CreatedAt      time.Time        `gorm:"not null"`
	UpdatedAt      time.Time        `gorm:"not null"`
}

// TableName pins the credentials table name.
func (Credential) TableName() string {
	return "credentials"
}

// CredentialPark is one transient cooldown park. Expired rows read as
// missing; cleanup removes them through the Until index.
type CredentialPark struct {
	CredentialID string     `gorm:"primaryKey"`
	Credential   Credential `gorm:"foreignKey:CredentialID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Reason       string     `gorm:"not null"`
	Until        time.Time  `gorm:"not null;index"`
	ParkedAt     time.Time  `gorm:"not null"`
}

// TableName pins the credential parks table name.
func (CredentialPark) TableName() string {
	return "credential_parks"
}

// CredentialHealth stamps the last detached health-check verdict.
type CredentialHealth struct {
	CredentialID string     `gorm:"primaryKey"`
	Credential   Credential `gorm:"foreignKey:CredentialID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	LastCheckAt  time.Time  `gorm:"not null"`
	LastStatus   string     `gorm:"not null"`
}

// TableName pins the credential health table name.
func (CredentialHealth) TableName() string {
	return "credential_health"
}
