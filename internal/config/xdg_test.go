package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortableDefaults(t *testing.T) {
	cases := map[string]string{
		"database":    DefaultDatabasePath,
		"cache":       DefaultCachePath,
		"events":      DefaultEventsPath,
		"dotenv":      DefaultDotenvPath,
		"devDatabase": DefaultDevDatabasePath,
		"devCache":    DefaultDevCachePath,
		"devEvents":   DefaultDevEventsPath,
		"devDotenv":   DefaultDevDotenvPath,
	}
	for name, value := range cases {
		if !strings.HasPrefix(value, "~/") && !strings.HasPrefix(value, `~\`) {
			t.Errorf("%s default = %q, want portable ~/ form", name, value)
		}
	}
	if strings.Contains(DefaultDatabasePath, ".dev.") {
		t.Error("prod database default carries .dev suffix")
	}
	for name, value := range map[string]string{
		"devDatabase": DefaultDevDatabasePath,
		"devCache":    DefaultDevCachePath,
		"devEvents":   DefaultDevEventsPath,
		"devDotenv":   DefaultDevDotenvPath,
	} {
		if !strings.Contains(value, ".dev") {
			t.Errorf("%s default = %q, want .dev suffix", name, value)
		}
	}
}

func TestApplicationDirectoriesHonorOverrides(t *testing.T) {
	clearProcessEnvironment(t)
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))

	configurationDirectory, err := ConfigurationApplicationDirectory()
	if err != nil {
		t.Fatalf("configuration directory: %v", err)
	}
	if want := filepath.Join(base, "config", "llm-router"); configurationDirectory != want {
		t.Errorf("configuration = %q, want %q", configurationDirectory, want)
	}
	dataDirectory, err := DataApplicationDirectory()
	if err != nil {
		t.Fatalf("data directory: %v", err)
	}
	if want := filepath.Join(base, "data", "llm-router"); dataDirectory != want {
		t.Errorf("data = %q, want %q", dataDirectory, want)
	}
}

func TestIsManagedPath(t *testing.T) {
	clearProcessEnvironment(t)
	home := t.TempDir()
	setHomeDirectory(t, home)

	managed, err := ExpandPath(DefaultDatabasePath)
	if err != nil {
		t.Fatalf("expand default: %v", err)
	}
	if !IsManagedPath(managed) {
		t.Errorf("IsManagedPath(%q) = false, want true", managed)
	}
	if IsManagedPath(filepath.Join(t.TempDir(), "custom", "primary.sqlite3")) {
		t.Error("IsManagedPath(custom) = true, want false")
	}
	if IsManagedPath(".") {
		t.Error("IsManagedPath(.) = true, want false")
	}
}

func TestEnsureParentCreatesManagedMissing(t *testing.T) {
	clearProcessEnvironment(t)
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", base)
	dataDirectory, err := DataApplicationDirectory()
	if err != nil {
		t.Fatalf("data directory: %v", err)
	}
	target := filepath.Join(dataDirectory, "llm-router.sqlite3")
	if err := EnsureParentDirectory(target); err != nil {
		t.Fatalf("EnsureParentDirectory: %v", err)
	}
	if info, err := os.Stat(dataDirectory); err != nil || !info.IsDir() {
		t.Fatalf("managed directory missing after ensure: %v", err)
	}
}

func TestEnsureParentRejectsCustomMissing(t *testing.T) {
	clearProcessEnvironment(t)
	setHomeDirectory(t, t.TempDir())
	target := filepath.Join(t.TempDir(), "absent-parent", "primary.sqlite3")
	if err := EnsureParentDirectory(target); !errors.Is(err, ErrParentMissing) {
		t.Fatalf("error = %v, want ErrParentMissing", err)
	}
}
