package record

import (
	"time"
)

// VirtualModel routes requests to its members in list order. ManagedBy
// splits into managed columns: provider-maintained rows resolve live.
type VirtualModel struct {
	ID                  string            `gorm:"primaryKey"`
	Name                string            `gorm:"not null"`
	NameKey             string            `gorm:"not null;uniqueIndex"`
	Description         string            `gorm:"not null"`
	Instruction         string            `gorm:"not null"`
	ManagedProviderID   *string           `gorm:""`
	ManagedProvider     *ProviderInstance `gorm:"foreignKey:ManagedProviderID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	ManagedEndpointSlug *string           `gorm:""`
	Disabled            bool              `gorm:"not null"`
	Version             int               `gorm:"not null"`
	CreatedAt           time.Time         `gorm:"not null"`
	UpdatedAt           time.Time         `gorm:"not null"`
}

// TableName pins the virtual models table name.
func (VirtualModel) TableName() string {
	return "virtual_models"
}

// VirtualModelMember is one fall-through step. Position keeps the priority
// order. Members pointing at another virtual model use the virtual provider
// row, so every member references a provider instance.
type VirtualModelMember struct {
	VirtualModelID string           `gorm:"primaryKey"`
	VirtualModel   VirtualModel     `gorm:"foreignKey:VirtualModelID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Position       int              `gorm:"primaryKey"`
	ProviderID     string           `gorm:"not null"`
	Provider       ProviderInstance `gorm:"foreignKey:ProviderID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	ModelName      string           `gorm:"not null"`
}

// TableName pins the virtual model members table name.
func (VirtualModelMember) TableName() string {
	return "virtual_model_members"
}
