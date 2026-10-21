package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupDatabaseCopiesMainFileAndWriteAheadLog(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "router.sqlite3")
	if err := os.WriteFile(databasePath, []byte("main"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(databasePath+"-wal", []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	backupPath, err := backupDatabase(databasePath)
	if err != nil || backupPath != databasePath+backupSuffix {
		t.Fatalf("backupDatabase = %q, %v", backupPath, err)
	}
	main, _ := os.ReadFile(backupPath)
	wal, _ := os.ReadFile(backupPath + "-wal")
	if string(main) != "main" || string(wal) != "wal" {
		t.Errorf("backup = %q and %q, want main and wal", main, wal)
	}
}

func TestBackupDatabaseDropsStaleLogCopy(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "router.sqlite3")
	if err := os.WriteFile(databasePath, []byte("main"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(databasePath+backupSuffix+"-wal", []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := backupDatabase(databasePath); err != nil {
		t.Fatalf("backupDatabase: %v", err)
	}
	if _, err := os.Stat(databasePath + backupSuffix + "-wal"); !os.IsNotExist(err) {
		t.Errorf("stale log copy survives: %v", err)
	}
}

func TestBackupDatabaseWithoutDatabase(t *testing.T) {
	backupPath, err := backupDatabase(filepath.Join(t.TempDir(), "absent.sqlite3"))
	if err != nil || backupPath != "" {
		t.Errorf("backupDatabase = %q, %v, want empty and nil", backupPath, err)
	}
}
