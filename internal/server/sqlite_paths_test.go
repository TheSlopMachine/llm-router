package server

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/config"
)

func TestResolveDatabasePaths(test *testing.T) {
	cases := []struct {
		name         string
		databasePath string
		sqlitePath   string
		wantBolt     string
		wantSQLite   string
	}{
		{
			name:         "sqlite path names sqlite with bbolt sibling",
			databasePath: filepath.Join("/tmp", "router", "llm-router-dev.sqlite3"),
			sqlitePath:   "",
			wantBolt:     filepath.Join("/tmp", "router", "llm-router-dev.db"),
			wantSQLite:   filepath.Join("/tmp", "router", "llm-router-dev.sqlite3"),
		},
		{
			name:         "bbolt path keeps bbolt with sqlite sibling",
			databasePath: filepath.Join("/tmp", "router", "llm-router.db"),
			sqlitePath:   "",
			wantBolt:     filepath.Join("/tmp", "router", "llm-router.db"),
			wantSQLite:   filepath.Join("/tmp", "router", "llm-router.sqlite3"),
		},
		{
			name:         "extensionless path appends sqlite suffix",
			databasePath: filepath.Join("/tmp", "router", "llm-router-dev"),
			sqlitePath:   "",
			wantBolt:     filepath.Join("/tmp", "router", "llm-router-dev"),
			wantSQLite:   filepath.Join("/tmp", "router", "llm-router-dev.sqlite3"),
		},
		{
			name:         "explicit override wins",
			databasePath: filepath.Join("/tmp", "router", "llm-router.db"),
			sqlitePath:   filepath.Join("/tmp", "other", "custom.sqlite3"),
			wantBolt:     filepath.Join("/tmp", "router", "llm-router.db"),
			wantSQLite:   filepath.Join("/tmp", "other", "custom.sqlite3"),
		},
		{
			name:         "blank override derives",
			databasePath: filepath.Join("/tmp", "router", "llm-router.db"),
			sqlitePath:   "   ",
			wantBolt:     filepath.Join("/tmp", "router", "llm-router.db"),
			wantSQLite:   filepath.Join("/tmp", "router", "llm-router.sqlite3"),
		},
	}
	for _, current := range cases {
		test.Run(current.name, func(test *testing.T) {
			boltPath, resolvedSQLitePath := resolveDatabasePaths(current.databasePath, current.sqlitePath)
			if boltPath != current.wantBolt {
				test.Fatalf("bolt path = %q, want %q", boltPath, current.wantBolt)
			}
			if resolvedSQLitePath != current.wantSQLite {
				test.Fatalf("sqlite path = %q, want %q", resolvedSQLitePath, current.wantSQLite)
			}
		})
	}
}

// TestNewOpensBothDatabases proves the integrator wiring: one Server opens
// bbolt and SQLite side by side with the GORM set threaded into the
// dashboard handler. Request paths keep serving bbolt until cutover.
func TestNewOpensBothDatabases(test *testing.T) {
	directory := test.TempDir()
	applicationConfig := &config.Config{
		DashboardAddr: "localhost:38080",
		APIAddr:       "localhost:38081",
		DBPath:        filepath.Join(directory, "llm-router.db"),
		LogLevel:      config.LogLevelInfo,
		NoAuth:        true,
	}
	applicationServer, err := New(applicationConfig, slog.Default())
	if err != nil {
		test.Fatalf("New with temp paths: %v", err)
	}
	defer applicationServer.Close()
	if applicationServer.database == nil {
		test.Fatal("bbolt handle must stay open until cutover")
	}
	if applicationServer.sqliteDatabase == nil || applicationServer.sqliteDatabase.Handle == nil {
		test.Fatal("sqlite handle must be open alongside bbolt")
	}
	wantSQLite := filepath.Join(directory, "llm-router.sqlite3")
	if applicationServer.sqlitePath != wantSQLite {
		test.Fatalf("sqlite path = %q, want %q", applicationServer.sqlitePath, wantSQLite)
	}
}
