//go:build unix

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDotenvPathSkipsUnreadableSystemFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	clearProcessEnvironment(t)
	home := t.TempDir()
	setHomeDirectory(t, home)
	systemDirectory := filepath.Join(t.TempDir(), "llm-router")
	if err := os.Mkdir(systemDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	systemPath := filepath.Join(systemDirectory, ".env")
	if err := os.WriteFile(systemPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// A package gives the directory to the service user with mode 0750.
	if err := os.Chmod(systemDirectory, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(systemDirectory, 0o755) })
	previousSystemPath := systemDotenvPath
	systemDotenvPath = systemPath
	t.Cleanup(func() { systemDotenvPath = previousSystemPath })

	path, err := ResolveDotenvPath()
	if err != nil {
		t.Fatalf("ResolveDotenvPath: %v", err)
	}
	if want := filepath.Join(home, ".config", "llm-router", ".env"); path != want {
		t.Errorf("path = %q, want the user default %q", path, want)
	}
	if IsSystemDotenvPath(path) {
		t.Error("the user default counts as the system file")
	}
}
