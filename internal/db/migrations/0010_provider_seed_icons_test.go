package migrations

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/migrate"
	"gorm.io/gorm"
)

func TestProviderSeedIconsClearsSeededRowsOnly(t *testing.T) {
	ctx := context.Background()
	primary := openIdentityHandle(t, filepath.Join(t.TempDir(), "seed-icons.sqlite3"))
	if err := migrate.Run(ctx, primary, RegistryDatabase[:8]); err != nil {
		t.Fatalf("apply earlier migrations: %v", err)
	}
	now := time.Now().UTC()
	rows := []providerInstanceSnapshot{
		{ID: "seeded", Name: "Seeded", TypeKey: "seeded", IsUIReadonly: true, IconURL: "https://example.com/seeded.png", CreatedAt: now, UpdatedAt: now},
		{ID: "custom", Name: "Custom", TypeKey: "custom", IconURL: "https://example.com/custom.png", CreatedAt: now, UpdatedAt: now},
	}
	for index := range rows {
		if err := gorm.G[providerInstanceSnapshot](primary).Create(ctx, &rows[index]); err != nil {
			t.Fatalf("seed provider row: %v", err)
		}
	}
	if err := migrate.Run(ctx, primary, RegistryDatabase); err != nil {
		t.Fatalf("apply provider seed icons migration: %v", err)
	}
	icons := map[string]string{}
	stored, err := gorm.G[providerInstanceSnapshot](primary).Find(ctx)
	if err != nil {
		t.Fatalf("read provider rows: %v", err)
	}
	for _, row := range stored {
		icons[row.ID] = row.IconURL
	}
	if icons["seeded"] != "" {
		t.Errorf("seeded icon = %q, want empty", icons["seeded"])
	}
	if icons["custom"] != "https://example.com/custom.png" {
		t.Errorf("custom icon = %q, want kept", icons["custom"])
	}
}
