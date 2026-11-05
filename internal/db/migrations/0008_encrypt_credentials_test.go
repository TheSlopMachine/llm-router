package migrations

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/credentialcipher"
	"github.com/TheSlopMachine/llm-router/internal/db/migrate"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestEncryptCredentialsSealsPlaintextRows(t *testing.T) {
	ctx := context.Background()
	if err := credentialcipher.Install(bytes.Repeat([]byte{0x11}, credentialcipher.KeySize)); err != nil {
		t.Fatal(err)
	}
	primary := openIdentityHandle(t, filepath.Join(t.TempDir(), "encrypt.sqlite3"))
	if err := migrate.Run(ctx, primary, RegistryDatabase[:6]); err != nil {
		t.Fatalf("apply earlier migrations: %v", err)
	}
	now := time.Now().UTC()
	provider := providerInstanceSnapshot{ID: "google", Name: "Google", TypeKey: "google", CreatedAt: now, UpdatedAt: now}
	if err := gorm.G[providerInstanceSnapshot](primary).Create(ctx, &provider); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	credential := credentialSnapshot{ID: "key-1", ProviderID: "google", Label: "main", Data: map[string]any{"api_key": "AIza-plaintext-secret"}, CreatedAt: now, UpdatedAt: now}
	if err := gorm.G[credentialSnapshot](primary).Create(ctx, &credential); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	empty := credentialSnapshot{ID: "key-2", ProviderID: "google", Label: "empty", CreatedAt: now, UpdatedAt: now}
	if err := gorm.G[credentialSnapshot](primary).Create(ctx, &empty); err != nil {
		t.Fatalf("seed empty credential: %v", err)
	}

	if err := migrate.Run(ctx, primary, RegistryDatabase[:7]); err != nil {
		t.Fatalf("apply encrypt migration: %v", err)
	}
	rows, err := gorm.G[credentialDataSnapshot](primary, clause.Eq{Column: "id", Value: "key-1"}).Find(ctx)
	if err != nil || len(rows) != 1 || rows[0].Data == nil {
		t.Fatalf("read sealed row: %v %+v", err, rows)
	}
	stored := *rows[0].Data
	if !strings.HasPrefix(stored, credentialcipher.SealedPrefix) || strings.Contains(stored, "AIza-plaintext-secret") {
		t.Fatalf("stored data must be sealed, got %q", stored)
	}
	current, _ := credentialcipher.Current()
	opened, err := current.Open(stored)
	if err != nil {
		t.Fatalf("open sealed data: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(opened, &decoded); err != nil || decoded["api_key"] != "AIza-plaintext-secret" {
		t.Fatalf("decoded data: %v %v", decoded, err)
	}

	// Re-running the body leaves sealed rows untouched.
	if err := upEncryptCredentials(primary); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	again, _ := gorm.G[credentialDataSnapshot](primary, clause.Eq{Column: "id", Value: "key-1"}).Find(ctx)
	if *again[0].Data != stored {
		t.Fatal("sealed rows must not be re-sealed")
	}
}
