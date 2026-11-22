// Provider virtual settings owns migration version 10 of the durable database.
//
// Version 10 creates provider_virtual_settings: one row per provider whose
// managed virtual models are switched off, with the endpoint slugs whose
// model was enabled. No row means on, so existing providers keep their
// managed virtual models.
package migrations

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

type providerVirtualSettingSnapshot struct {
	ProviderID       string                   `gorm:"primaryKey"`
	Provider         providerInstanceSnapshot `gorm:"foreignKey:ProviderID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Disabled         bool                     `gorm:"not null"`
	EnabledEndpoints []string                 `gorm:"serializer:json;not null"`
	UpdatedAt        time.Time                `gorm:"not null"`
}

// TableName pins the provider virtual settings table name.
func (providerVirtualSettingSnapshot) TableName() string {
	return "provider_virtual_settings"
}

func upProviderVirtualSettings(transaction *gorm.DB) error {
	migrator := transaction.Migrator()
	if migrator.HasTable(&providerVirtualSettingSnapshot{}) {
		return nil
	}
	if err := migrator.CreateTable(&providerVirtualSettingSnapshot{}); err != nil {
		return fmt.Errorf("create provider_virtual_settings: %w", err)
	}
	return nil
}
