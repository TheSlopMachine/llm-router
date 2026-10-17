// Proxy switches owns migration version 5 of the durable database.
//
// Version 5 adds the two proxy on/off switches to the instance settings row,
// stored inverted so existing rows keep the proxy subsystem enabled.
package migrations

import (
	"fmt"

	"gorm.io/gorm"
)

type instanceProxySwitchesSnapshot struct {
	ID                int64 `gorm:"primaryKey;check:id = 1"`
	ProxyDisabled     bool  `gorm:"not null;default:false"`
	ProxyPoolDisabled bool  `gorm:"not null;default:false"`
}

// TableName pins the instance settings table name.
func (instanceProxySwitchesSnapshot) TableName() string {
	return "instance_settings"
}

func upProxySwitches(transaction *gorm.DB) error {
	migrator := transaction.Migrator()
	for _, column := range []string{"proxy_disabled", "proxy_pool_disabled"} {
		if migrator.HasColumn(&instanceProxySwitchesSnapshot{}, column) {
			continue
		}
		if err := migrator.AddColumn(&instanceProxySwitchesSnapshot{}, column); err != nil {
			return fmt.Errorf("add instance_settings.%s: %w", column, err)
		}
	}
	return nil
}
