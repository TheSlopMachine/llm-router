package record

import (
	"time"
)

// ProxyPool is one named proxy pool. The router seeds the built-in pool auto.
type ProxyPool struct {
	ID        string    `gorm:"primaryKey"`
	Name      string    `gorm:"not null"`
	Builtin   bool      `gorm:"not null"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

// TableName pins the proxy pools table name.
func (ProxyPool) TableName() string {
	return "proxy_pools"
}

// ProxyPoolEntry is one manual entry in a custom pool. Position preserves order.
type ProxyPoolEntry struct {
	PoolID   string    `gorm:"primaryKey"`
	Pool     ProxyPool `gorm:"foreignKey:PoolID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Position int       `gorm:"primaryKey"`
	URL      string    `gorm:"not null"`
	Country  string    `gorm:"not null"`
}

// TableName pins the proxy pool entries table name.
func (ProxyPoolEntry) TableName() string {
	return "proxy_pool_entries"
}

// ProxyState is one cached free-pool proxy keyed by canonical URL.
// Metadata stays JSON because the shape is user free-form.
type ProxyState struct {
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
func (ProxyState) TableName() string {
	return "proxy_states"
}
