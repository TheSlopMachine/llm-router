// Plugin disabled state owns migration version 3 of the durable database.
//
// Version 1 stays untouched. Version 3 adds the operator-owned disabled flag
// to the plugins table so dashboard toggles persist across restarts.
// Disabled plugins stay inventoried but leave the in-memory type registry.
package migrations

import (
	"fmt"

	"gorm.io/gorm"
)

type pluginDisabledSnapshot struct {
	ID       string `gorm:"primaryKey"`
	Disabled bool   `gorm:"not null;default:false"`
}

// TableName pins the plugins table name.
func (pluginDisabledSnapshot) TableName() string {
	return "plugins"
}

func upPluginDisabled(transaction *gorm.DB) error {
	migrator := transaction.Migrator()
	if migrator.HasColumn(&pluginDisabledSnapshot{}, "disabled") {
		return nil
	}
	if err := migrator.AddColumn(&pluginDisabledSnapshot{}, "disabled"); err != nil {
		return fmt.Errorf("add plugins.disabled: %w", err)
	}
	return nil
}
