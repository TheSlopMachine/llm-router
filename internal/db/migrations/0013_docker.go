// Docker owns migration version 12 of the durable database.
//
// Version 12 creates the Docker tables: docker_connections (one row, the
// daemon endpoint and its sealed TLS and SSH material), docker_registry_
// credentials (sealed registry logins per host). Secret columns hold
// credential_cipher sealed text.
package migrations

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

type dockerConnectionSnapshot struct {
	ID           int64     `gorm:"primaryKey;check:id = 1"`
	Endpoint     string    `gorm:"not null"`
	Namespace    string    `gorm:"not null"`
	DialMode     string    `gorm:"not null;check:dial_mode IN ('auto','route','relay')"`
	GatewayImage string    `gorm:"not null"`
	SSHHostKey   string    `gorm:"column:ssh_host_key;not null"`
	Secrets      string    `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

// TableName pins the docker connections table name.
func (dockerConnectionSnapshot) TableName() string {
	return "docker_connections"
}

type dockerRegistryCredentialSnapshot struct {
	Host      string    `gorm:"primaryKey"`
	Username  string    `gorm:"not null"`
	Secret    string    `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

// TableName pins the docker registry credentials table name.
func (dockerRegistryCredentialSnapshot) TableName() string {
	return "docker_registry_credentials"
}

func upDocker(transaction *gorm.DB) error {
	migrator := transaction.Migrator()
	for _, table := range []any{&dockerConnectionSnapshot{}, &dockerRegistryCredentialSnapshot{}} {
		if migrator.HasTable(table) {
			continue
		}
		if err := migrator.CreateTable(table); err != nil {
			return fmt.Errorf("create docker table: %w", err)
		}
	}
	return nil
}
