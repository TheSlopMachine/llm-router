// Events schema owns migration version 1 of the events file plus the
// prune helper inputs. The events file holds operator telemetry only:
// event records with free-form metadata keys. Deleting the file while
// stopped loses log history and nothing else.
package migrations

import (
	"fmt"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/migrate"
	"gorm.io/gorm"
)

// RegistryEvents lists every migration for the events database file in
// order. Version numbers are scoped per file.
var RegistryEvents = []migrate.Migration{
	{Version: 1, Name: "events_schema", Up: upEventsSchema},
}

type eventRecordSnapshot struct {
	ID        int64     `gorm:"primaryKey;autoIncrement"`
	Timestamp time.Time `gorm:"not null;index:idx_event_records_timestamp"`
	Severity  int       `gorm:"not null;index:idx_event_records_severity_timestamp"`
	Message   string    `gorm:"not null"`
}

// TableName pins the event records table name.
func (eventRecordSnapshot) TableName() string {
	return "event_records"
}

type eventKeySnapshot struct {
	RecordID int64               `gorm:"primaryKey"`
	Record   eventRecordSnapshot `gorm:"foreignKey:RecordID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Key      string              `gorm:"primaryKey;index:idx_event_keys_key_value,priority:1"`
	Value    *string             `gorm:"index:idx_event_keys_key_value,priority:2"`
}

// TableName pins the event keys table name.
func (eventKeySnapshot) TableName() string {
	return "event_keys"
}

func upEventsSchema(transaction *gorm.DB) error {
	migrator := transaction.Migrator()
	for _, table := range []any{
		&eventRecordSnapshot{},
		&eventKeySnapshot{},
	} {
		if err := migrator.CreateTable(table); err != nil {
			return fmt.Errorf("create table: %w", err)
		}
	}
	return nil
}
