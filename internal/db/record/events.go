package record

import (
	"time"
)

// EventRecord is one persisted log event. Severity follows 0=DEBUG,
// 1=INFO, 2=WARN, 3=ERROR, 4=FATAL. Timestamp stores UTC.
type EventRecord struct {
	ID        int64     `gorm:"primaryKey;autoIncrement"`
	Timestamp time.Time `gorm:"not null;index:idx_event_records_timestamp"`
	Severity  int       `gorm:"not null;index:idx_event_records_severity_timestamp"`
	Message   string    `gorm:"not null"`
}

// TableName pins the event records table name.
func (EventRecord) TableName() string {
	return "event_records"
}

// EventKey is one metadata entry on an event record. Value is nullable:
// a null value marks key existence without a value check.
type EventKey struct {
	RecordID int64       `gorm:"primaryKey"`
	Record   EventRecord `gorm:"foreignKey:RecordID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Key      string      `gorm:"primaryKey;index:idx_event_keys_key_value,priority:1"`
	Value    *string     `gorm:"index:idx_event_keys_key_value,priority:2"`
}

// TableName pins the event keys table name.
func (EventKey) TableName() string {
	return "event_keys"
}
