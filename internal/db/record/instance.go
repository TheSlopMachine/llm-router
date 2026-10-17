// Package record owns GORM persistence structs. Wire types live in
// internal/models. Converters stay in services and remain explicit.
package record

import (
	"time"
)

// InstanceSetting is the single instance row. The identifier is always 1.
type InstanceSetting struct {
	ID               int64  `gorm:"primaryKey;check:id = 1"`
	Bootstrapped     bool   `gorm:"not null"`
	IsClusterNode    bool   `gorm:"not null"`
	DisableTelemetry bool   `gorm:"not null"`
	ModelsFilter     string `gorm:"not null;check:models_filter IN ('all','enabled','disabled')"`
	ProxyMaxChecks   int    `gorm:"not null;default:0"`
	// ProxyDisabled and ProxyPoolDisabled store the proxy switches inverted so
	// that a zero row (fresh install, bootstrap) keeps the proxy subsystem on.
	ProxyDisabled     bool      `gorm:"not null;default:false"`
	ProxyPoolDisabled bool      `gorm:"not null;default:false"`
	UpdatedAt         time.Time `gorm:"not null"`
}

// TableName pins the instance settings table name.
func (InstanceSetting) TableName() string {
	return "instance_settings"
}

// AdminUser is one dashboard operator account.
type AdminUser struct {
	Username     string    `gorm:"primaryKey"`
	PasswordHash string    `gorm:"not null"`
	CreatedAt    time.Time `gorm:"not null"`
}

// TableName pins the admin users table name.
func (AdminUser) TableName() string {
	return "admin_users"
}

// Session is one dashboard session. The token hash is the primary key so the
// plaintext token never lands in storage. Username is a soft reference to
// the admin account: sessions live in the cache file, which cannot enforce
// foreign keys into the durable database file.
type Session struct {
	TokenHash  string    `gorm:"primaryKey"`
	Username   string    `gorm:"not null;index"`
	CreatedAt  time.Time `gorm:"not null"`
	ExpiresAt  time.Time `gorm:"not null;index"`
	RememberMe bool      `gorm:"not null"`
}

// TableName pins the sessions table name.
func (Session) TableName() string {
	return "sessions"
}
