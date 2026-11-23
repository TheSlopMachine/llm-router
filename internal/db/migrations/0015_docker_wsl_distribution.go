// Docker WSL distribution owns migration version 14 of the durable database.
//
// Version 14 adds docker_connections.wsl_distribution: the WSL distro a
// Windows router keeps running for a Docker daemon inside it. Existing rows
// get an empty value, which keeps no distro running.
package migrations

import (
	"fmt"

	"gorm.io/gorm"
)

type dockerConnectionWSLSnapshot struct {
	WSLDistribution string `gorm:"column:wsl_distribution;not null;default:''"`
}

// TableName pins the docker connections table name.
func (dockerConnectionWSLSnapshot) TableName() string {
	return "docker_connections"
}

func upDockerWSLDistribution(transaction *gorm.DB) error {
	migrator := transaction.Migrator()
	if migrator.HasColumn(&dockerConnectionWSLSnapshot{}, "wsl_distribution") {
		return nil
	}
	if err := migrator.AddColumn(&dockerConnectionWSLSnapshot{}, "WSLDistribution"); err != nil {
		return fmt.Errorf("add docker_connections.wsl_distribution: %w", err)
	}
	return nil
}
