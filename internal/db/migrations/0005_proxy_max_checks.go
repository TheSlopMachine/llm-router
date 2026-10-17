// Proxy max checks owns migration version 4 of the durable database.
//
// Version 4 adds the operator-chosen cap on parallel proxy checks to the
// instance settings row. Zero keeps the adaptive default.
package migrations

import (
	"fmt"

	"gorm.io/gorm"
)

type instanceProxyMaxChecksSnapshot struct {
	ID             int64 `gorm:"primaryKey;check:id = 1"`
	ProxyMaxChecks int   `gorm:"not null;default:0"`
}

// TableName pins the instance settings table name.
func (instanceProxyMaxChecksSnapshot) TableName() string {
	return "instance_settings"
}

func upProxyMaxChecks(transaction *gorm.DB) error {
	migrator := transaction.Migrator()
	if migrator.HasColumn(&instanceProxyMaxChecksSnapshot{}, "proxy_max_checks") {
		return nil
	}
	if err := migrator.AddColumn(&instanceProxyMaxChecksSnapshot{}, "proxy_max_checks"); err != nil {
		return fmt.Errorf("add instance_settings.proxy_max_checks: %w", err)
	}
	return nil
}
