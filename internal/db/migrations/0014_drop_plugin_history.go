// Drop plugin history owns migration version 13 of the durable database.
//
// Plugin rollback was removed, and with it the plugin_versions table that
// kept up to ten previous sources per plugin for it. Version 13 drops that
// table. It also drops plugin_api_grants, which pre-release builds of
// version 12 created for a per-plugin Docker switch that was replaced by
// approval at install.
package migrations

import (
	"fmt"

	"gorm.io/gorm"
)

func upDropPluginHistory(transaction *gorm.DB) error {
	migrator := transaction.Migrator()
	for _, table := range []string{"plugin_versions", "plugin_api_grants"} {
		if !migrator.HasTable(table) {
			continue
		}
		if err := migrator.DropTable(table); err != nil {
			return fmt.Errorf("drop %s: %w", table, err)
		}
	}
	return nil
}
