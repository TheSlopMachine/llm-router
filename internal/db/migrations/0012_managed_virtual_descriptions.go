// Managed virtual descriptions owns migration version 11 of the durable database.
//
// Provider-managed virtual models were created with the description
// "Automatic fall-through across <provider> <endpoint> models. Members refresh
// on import." New ones read "Fall-through across <provider> <endpoint>
// models." Version 11 rewrites stored descriptions in the old form; edited
// descriptions stay.
package migrations

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	legacyManagedDescriptionPrefix = "Automatic fall-through across "
	legacyManagedDescriptionSuffix = " models. Members refresh on import."
)

// shortManagedDescription returns the new description for an old one, and
// false for any other text.
func shortManagedDescription(description string) (string, bool) {
	if !strings.HasPrefix(description, legacyManagedDescriptionPrefix) || !strings.HasSuffix(description, legacyManagedDescriptionSuffix) {
		return "", false
	}
	subject := strings.TrimSuffix(strings.TrimPrefix(description, legacyManagedDescriptionPrefix), legacyManagedDescriptionSuffix)
	return "Fall-through across " + subject + " models.", true
}

func upManagedVirtualDescriptions(transaction *gorm.DB) error {
	ctx := context.Background()
	rows, err := gorm.G[virtualModelSnapshot](transaction).Find(ctx)
	if err != nil {
		return fmt.Errorf("read virtual models: %w", err)
	}
	for _, row := range rows {
		if row.ManagedProviderID == nil {
			continue
		}
		description, ok := shortManagedDescription(row.Description)
		if !ok {
			continue
		}
		if _, err := gorm.G[virtualModelSnapshot](transaction, clause.Eq{Column: "id", Value: row.ID}).Update(ctx, "description", description); err != nil {
			return fmt.Errorf("rewrite description of virtual model %q: %w", row.ID, err)
		}
	}
	return nil
}
