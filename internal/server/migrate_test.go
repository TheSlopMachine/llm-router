package server

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	bolt "go.etcd.io/bbolt"
)

func TestMigrateClearCredentialQuota(t *testing.T) {
	database := testutil.SetupTestDB(t)
	legacy := map[string]any{
		"id": "c1", "provider_id": "p", "label": "l",
		"data":           map[string]any{"api_key": "k"},
		"quota_reset_at": "2026-01-01T00:00:00Z",
	}
	enc, _ := json.Marshal(legacy)
	if err := database.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(db.BucketCredentials).Put([]byte("c1"), enc)
	}); err != nil {
		t.Fatalf("seed legacy credential: %v", err)
	}
	migrateClearCredentialQuota(slog.Default(), database)
	if err := database.View(func(tx *bolt.Tx) error {
		var row map[string]any
		if err := json.Unmarshal(tx.Bucket(db.BucketCredentials).Get([]byte("c1")), &row); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if _, ok := row["quota_reset_at"]; ok {
			t.Fatal("quota_reset_at must be stripped")
		}
		if row["label"] != "l" {
			t.Fatalf("other fields must survive: %v", row)
		}
		return nil
	}); err != nil {
		t.Fatalf("view: %v", err)
	}
	migrateClearCredentialQuota(slog.Default(), database)
}
