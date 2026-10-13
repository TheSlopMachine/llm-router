package db_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"gorm.io/gorm/clause"
)

func TestSessionSurvivesAdminUserDelete(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	database, err := db.OpenSQLite(ctx, filepath.Join(directory, "cascade.sqlite3"), filepath.Join(directory, "cascade.cache.sqlite3"), filepath.Join(directory, "cascade.events.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	users := repository.NewGormRepository[record.AdminUser](database.Handle, "username")
	sessions := repository.NewGormRepository[record.Session](database.Cache, "token_hash")
	now := time.Now().UTC()
	if err := users.Upsert(ctx, record.AdminUser{Username: "operator", PasswordHash: "hash", CreatedAt: now}); err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	if err := sessions.Upsert(ctx, record.Session{TokenHash: "abc", Username: "operator", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	if _, err := users.Delete(ctx, clause.Eq{Column: "username", Value: "operator"}); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	count, err := sessions.Count(ctx)
	if err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != 1 {
		t.Errorf("sessions hold %d rows after user delete, want 1 without cross-file cascade", count)
	}
}

func TestSessionAcceptsMissingAdminUser(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	database, err := db.OpenSQLite(ctx, filepath.Join(directory, "foreign.sqlite3"), filepath.Join(directory, "foreign.cache.sqlite3"), filepath.Join(directory, "foreign.events.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	sessions := repository.NewGormRepository[record.Session](database.Cache, "token_hash")
	now := time.Now().UTC()
	if err := sessions.Upsert(ctx, record.Session{TokenHash: "orphan", Username: "missing", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Errorf("orphan session upsert failed: %v, want success with soft username reference", err)
	}
}
