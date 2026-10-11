package record

import (
	"time"
)

// RouterToken is one issued bearer token. Rules normalize into child rows.
type RouterToken struct {
	ID                  string    `gorm:"primaryKey"`
	Name                string    `gorm:"not null"`
	TokenHash           string    `gorm:"not null;uniqueIndex"`
	AllowAllProviders   bool      `gorm:"not null"`
	AllowAllModels      bool      `gorm:"not null"`
	AllowAllCredentials bool      `gorm:"not null"`
	CreatedAt           time.Time `gorm:"not null"`
}

// TableName pins the router tokens table name.
func (RouterToken) TableName() string {
	return "router_tokens"
}

// TokenAllowedProvider pins one provider on a token allow-list.
type TokenAllowedProvider struct {
	TokenID    string           `gorm:"primaryKey"`
	Token      RouterToken      `gorm:"foreignKey:TokenID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	ProviderID string           `gorm:"primaryKey"`
	Provider   ProviderInstance `gorm:"foreignKey:ProviderID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
}

// TableName pins the token allowed providers table name.
func (TokenAllowedProvider) TableName() string {
	return "token_allowed_providers"
}

// TokenAllowedModel pins one provider/model on a token allow-list.
// The model identifier is a soft reference: provider/model strings outlive
// catalog rows, so no foreign key guards them.
type TokenAllowedModel struct {
	TokenID string      `gorm:"primaryKey"`
	Token   RouterToken `gorm:"foreignKey:TokenID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	ModelID string      `gorm:"primaryKey"`
}

// TableName pins the token allowed models table name.
func (TokenAllowedModel) TableName() string {
	return "token_allowed_models"
}

// TokenAllowedCredential pins one credential on a token allow-list.
type TokenAllowedCredential struct {
	TokenID      string      `gorm:"primaryKey"`
	Token        RouterToken `gorm:"foreignKey:TokenID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	CredentialID string      `gorm:"primaryKey"`
	Credential   Credential  `gorm:"foreignKey:CredentialID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
}

// TableName pins the token allowed credentials table name.
func (TokenAllowedCredential) TableName() string {
	return "token_allowed_credentials"
}

// TokenCredentialScope grants a token access to one provider's credentials:
// all of them when AllCredentials reads true, pinned ones otherwise.
type TokenCredentialScope struct {
	ID             string           `gorm:"primaryKey"`
	TokenID        string           `gorm:"not null;uniqueIndex:idx_token_scopes_provider"`
	Token          RouterToken      `gorm:"foreignKey:TokenID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	ProviderID     string           `gorm:"not null;uniqueIndex:idx_token_scopes_provider"`
	Provider       ProviderInstance `gorm:"foreignKey:ProviderID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	AllCredentials bool             `gorm:"not null"`
}

// TableName pins the token credential scopes table name.
func (TokenCredentialScope) TableName() string {
	return "token_credential_scopes"
}

// TokenScopeCredential pins one credential inside a scope.
type TokenScopeCredential struct {
	ScopeID      string               `gorm:"primaryKey"`
	Scope        TokenCredentialScope `gorm:"foreignKey:ScopeID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	CredentialID string               `gorm:"primaryKey"`
	Credential   Credential           `gorm:"foreignKey:CredentialID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
}

// TableName pins the token scope credentials table name.
func (TokenScopeCredential) TableName() string {
	return "token_scope_credentials"
}
