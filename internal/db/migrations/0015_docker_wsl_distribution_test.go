package migrations

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/db/migrate"
)

func TestDockerWSLDistributionKeepsExistingConnection(t *testing.T) {
	ctx := context.Background()
	primary := openIdentityHandle(t, filepath.Join(t.TempDir(), "docker-wsl.sqlite3"))
	if err := migrate.Run(ctx, primary, RegistryDatabase[:13]); err != nil {
		t.Fatalf("apply earlier migrations: %v", err)
	}
	if err := primary.Exec("INSERT INTO docker_connections (id, endpoint, namespace, dial_mode, gateway_image, ssh_host_key, secrets, updated_at) VALUES (1, 'tcp://127.0.0.1:2375', 'llm-router', 'auto', '', '', '', CURRENT_TIMESTAMP)").Error; err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	if err := migrate.Run(ctx, primary, RegistryDatabase); err != nil {
		t.Fatalf("apply docker wsl distribution migration: %v", err)
	}
	var distribution string
	if err := primary.Raw("SELECT wsl_distribution FROM docker_connections WHERE id = 1").Scan(&distribution).Error; err != nil {
		t.Fatalf("read wsl_distribution: %v", err)
	}
	if distribution != "" {
		t.Fatalf("existing connection keeps a distro: %q", distribution)
	}
}
