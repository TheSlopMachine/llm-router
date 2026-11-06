// Dashboard password owns migration version 8 of the durable database and
// version 2 of the cache database.
//
// The dashboard has one operator and no accounts: one password hash lives in
// the single-row dashboard_password table. Durable version 8 creates it, moves
// one hash out of admin_users (the "admin" account, else the oldest one), then
// drops admin_users and the instance_settings.bootstrapped flag: a stored hash
// is the only "configured" state. Cache version 2 recreates sessions without
// the username column, so existing dashboard sessions end once.
package migrations

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type dashboardPasswordSnapshot struct {
	ID           int64     `gorm:"primaryKey;check:id = 1"`
	PasswordHash string    `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

// TableName pins the dashboard password table name.
func (dashboardPasswordSnapshot) TableName() string {
	return "dashboard_password"
}

type instanceBootstrappedSnapshot struct {
	ID           int64 `gorm:"primaryKey;check:id = 1"`
	Bootstrapped bool  `gorm:"not null"`
}

// TableName pins the instance settings table name.
func (instanceBootstrappedSnapshot) TableName() string {
	return "instance_settings"
}

// legacyDashboardAccount is the account whose hash becomes the dashboard
// password: set-dashboard-password always wrote this name.
const legacyDashboardAccount = "admin"

func upDashboardPassword(transaction *gorm.DB) error {
	ctx := context.Background()
	migrator := transaction.Migrator()
	if !migrator.HasTable(&dashboardPasswordSnapshot{}) {
		if err := migrator.CreateTable(&dashboardPasswordSnapshot{}); err != nil {
			return fmt.Errorf("create dashboard_password: %w", err)
		}
	}
	if migrator.HasTable(&adminUserSnapshot{}) {
		accounts, err := gorm.G[adminUserSnapshot](transaction).Find(ctx)
		if err != nil {
			return fmt.Errorf("read admin_users: %w", err)
		}
		if chosen, ok := chooseDashboardAccount(accounts); ok {
			row := dashboardPasswordSnapshot{ID: 1, PasswordHash: chosen.PasswordHash, UpdatedAt: time.Now().UTC()}
			if err := gorm.G[dashboardPasswordSnapshot](transaction).Create(ctx, &row); err != nil {
				return fmt.Errorf("store dashboard password: %w", err)
			}
		}
		if err := migrator.DropTable(&adminUserSnapshot{}); err != nil {
			return fmt.Errorf("drop admin_users: %w", err)
		}
	}
	if migrator.HasColumn(&instanceBootstrappedSnapshot{}, "bootstrapped") {
		if err := migrator.DropColumn(&instanceBootstrappedSnapshot{}, "bootstrapped"); err != nil {
			return fmt.Errorf("drop instance_settings.bootstrapped: %w", err)
		}
	}
	return nil
}

// chooseDashboardAccount picks the "admin" account, else the oldest one.
func chooseDashboardAccount(accounts []adminUserSnapshot) (adminUserSnapshot, bool) {
	var chosen adminUserSnapshot
	found := false
	for _, account := range accounts {
		if account.Username == legacyDashboardAccount {
			return account, true
		}
		if !found || account.CreatedAt.Before(chosen.CreatedAt) {
			chosen, found = account, true
		}
	}
	return chosen, found
}

type cacheSessionWithoutUsername struct {
	TokenHash  string    `gorm:"primaryKey"`
	CreatedAt  time.Time `gorm:"not null"`
	ExpiresAt  time.Time `gorm:"not null;index"`
	RememberMe bool      `gorm:"not null"`
}

// TableName pins the sessions table name.
func (cacheSessionWithoutUsername) TableName() string {
	return "sessions"
}

func upCacheSessionWithoutUsername(transaction *gorm.DB) error {
	migrator := transaction.Migrator()
	if err := migrator.DropTable(&cacheSessionWithoutUsername{}); err != nil {
		return fmt.Errorf("drop sessions: %w", err)
	}
	if err := migrator.CreateTable(&cacheSessionWithoutUsername{}); err != nil {
		return fmt.Errorf("create sessions: %w", err)
	}
	return nil
}
