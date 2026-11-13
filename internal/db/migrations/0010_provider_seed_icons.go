// Provider seed icons owns migration version 9 of the durable database.
//
// Seeded provider rows (UI-readonly) held a copy of the icon declared by
// their plugin type. The copy never followed plugin updates, so the icon now
// resolves from the plugin type at read time and seeded rows keep none.
package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func upProviderSeedIcons(transaction *gorm.DB) error {
	_, err := gorm.G[providerInstanceSnapshot](transaction, clause.Eq{Column: "is_ui_readonly", Value: true}).
		Update(context.Background(), "icon_url", "")
	if err != nil {
		return fmt.Errorf("clear seeded provider icons: %w", err)
	}
	return nil
}
