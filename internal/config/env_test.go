package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearProcessEnvironment removes every variable Load reads so ambient values
// and repository files cannot leak into hermetic cases. Variables are unset
// (not blanked) so dotenv loading fills them as in production.
func clearProcessEnvironment(environment *testing.T) {
	environment.Helper()
	for _, name := range []string{"HOST", "PORT", "ADMIN_PORT", "DB_PATH", "DEV_DB", "CACHE_PATH", "DEV_CACHE", "EVENTS_PATH", "DEV_EVENTS", "EVENTS_RETENTION_DAYS", "LOG_LEVEL", "NO_AUTH", "DEV_UI_REDIRECT"} {
		value, present := os.LookupEnv(name)
		if !present {
			continue
		}
		if err := os.Unsetenv(name); err != nil {
			environment.Fatalf("unset %s: %v", name, err)
		}
		restoreValue, restorePresent := value, present
		environment.Cleanup(func() {
			if restorePresent {
				_ = os.Setenv(name, restoreValue)
			}
		})
	}
}

// isolateDotenv points ENV_FILE at a missing path under temp.
func isolateDotenv(environment *testing.T) {
	environment.Helper()
	environment.Setenv("ENV_FILE", filepath.Join(environment.TempDir(), "missing.env"))
}

func TestLoadDefaults(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)

	configuration, loadedPath, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loadedPath != "" {
		t.Fatalf("loaded path = %q, want empty", loadedPath)
	}
	if configuration.Addr != "localhost:8080" {
		t.Errorf("addr = %q, want localhost:8080", configuration.Addr)
	}
	if configuration.DBPath != DefaultDatabasePath {
		t.Errorf("database = %q, want %q", configuration.DBPath, DefaultDatabasePath)
	}
	if configuration.CachePath != DefaultCachePath {
		t.Errorf("cache = %q, want %q", configuration.CachePath, DefaultCachePath)
	}
	if configuration.EventsPath != DefaultEventsPath {
		t.Errorf("events = %q, want %q", configuration.EventsPath, DefaultEventsPath)
	}
	if configuration.EventsRetentionDays != DefaultEventsRetentionDays {
		t.Errorf("retention = %d, want %d", configuration.EventsRetentionDays, DefaultEventsRetentionDays)
	}
	if configuration.LogLevel != LogLevelInfo {
		t.Errorf("log level = %q, want info", configuration.LogLevel)
	}
	if configuration.AdminAddr != "" {
		t.Errorf("admin addr = %q, want empty (disabled by default)", configuration.AdminAddr)
	}
}

func TestLoadIgnoresLegacySplitPorts(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	t.Setenv("WEB_PORT", "39080")
	t.Setenv("API_PORT", "39081")
	t.Setenv("PORT", "38080")

	configuration, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if configuration.Addr != "localhost:38080" {
		t.Errorf("addr = %q, want localhost:38080", configuration.Addr)
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	t.Setenv("PORT", "99999")

	if _, _, err := Load(); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("error = %v, want ErrInvalidPort", err)
	}
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	t.Setenv("LOG_LEVEL", "verbose")

	if _, _, err := Load(); !errors.Is(err, ErrInvalidLogLevel) {
		t.Fatalf("error = %v, want ErrInvalidLogLevel", err)
	}
}

func TestLoadIgnoresLegacyNoAuth(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	t.Setenv("NO_AUTH", "sometimes")

	// NO_AUTH is legacy: any value is ignored, never an error.
	if _, _, err := Load(); err != nil {
		t.Fatalf("Load: %v, want ignored legacy NO_AUTH", err)
	}
}

func TestAdminAddrBindsLoopbackRegardlessOfHost(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	t.Setenv("HOST", "0.0.0.0")
	t.Setenv("PORT", "8080")
	t.Setenv("ADMIN_PORT", "38082")

	configuration, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if configuration.Addr != "0.0.0.0:8080" {
		t.Errorf("addr = %q, want 0.0.0.0:8080", configuration.Addr)
	}
	if configuration.AdminAddr != "127.0.0.1:38082" {
		t.Errorf("admin addr = %q, want 127.0.0.1:38082", configuration.AdminAddr)
	}
}

func TestLoadRejectsAdminPortEqualToPort(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	t.Setenv("PORT", "38082")
	t.Setenv("ADMIN_PORT", "38082")

	if _, _, err := Load(); err == nil {
		t.Fatal("Load succeeded with ADMIN_PORT == PORT, want error")
	}
}

func TestLoadRejectsInvalidAdminPort(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	t.Setenv("ADMIN_PORT", "99999")

	if _, _, err := Load(); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("error = %v, want ErrInvalidPort", err)
	}
}

func TestLoadAcceptsWarningAlias(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	t.Setenv("LOG_LEVEL", "warning")

	configuration, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if configuration.LogLevel != LogLevelWarn {
		t.Errorf("log level = %q, want warn", configuration.LogLevel)
	}
}

func TestLoadPrefersDatabasePathOverDevDatabase(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	t.Setenv("DB_PATH", "primary.sqlite3")
	t.Setenv("DEV_DB", "fallback.sqlite3")

	configuration, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if configuration.DBPath != "primary.sqlite3" {
		t.Errorf("database = %q, want primary.sqlite3", configuration.DBPath)
	}
}

func TestLoadFallsBackToDevDatabase(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	t.Setenv("DEV_DB", "fallback.sqlite3")

	configuration, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if configuration.DBPath != "fallback.sqlite3" {
		t.Errorf("database = %q, want fallback.sqlite3", configuration.DBPath)
	}
}

func TestLoadPrefersCachePathOverDevCache(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	t.Setenv("CACHE_PATH", "primary.cache.sqlite3")
	t.Setenv("DEV_CACHE", "fallback.cache.sqlite3")

	configuration, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if configuration.CachePath != "primary.cache.sqlite3" {
		t.Errorf("cache = %q, want primary.cache.sqlite3", configuration.CachePath)
	}
}

func TestLoadFallsBackToDevCache(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	t.Setenv("DEV_CACHE", "fallback.cache.sqlite3")

	configuration, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if configuration.CachePath != "fallback.cache.sqlite3" {
		t.Errorf("cache = %q, want fallback.cache.sqlite3", configuration.CachePath)
	}
}

func TestLoadPrefersEventsPathOverDevEvents(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	t.Setenv("EVENTS_PATH", "primary.events.sqlite3")
	t.Setenv("DEV_EVENTS", "fallback.events.sqlite3")

	configuration, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if configuration.EventsPath != "primary.events.sqlite3" {
		t.Errorf("events = %q, want primary.events.sqlite3", configuration.EventsPath)
	}
}

func TestLoadFallsBackToDevEvents(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	t.Setenv("DEV_EVENTS", "fallback.events.sqlite3")

	configuration, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if configuration.EventsPath != "fallback.events.sqlite3" {
		t.Errorf("events = %q, want fallback.events.sqlite3", configuration.EventsPath)
	}
}

func TestLoadRejectsInvalidEventsRetention(t *testing.T) {
	for _, raw := range []string{"0", "-1", "bogus"} {
		clearProcessEnvironment(t)
		isolateDotenv(t)
		t.Setenv("EVENTS_RETENTION_DAYS", raw)

		if _, _, err := Load(); err == nil {
			t.Fatalf("Load with EVENTS_RETENTION_DAYS=%q succeeded, want error", raw)
		}
	}
}

func TestLoadReadsDotenvFile(t *testing.T) {
	clearProcessEnvironment(t)
	directory := t.TempDir()
	dotenvPath := filepath.Join(directory, ".env")
	if err := os.WriteFile(dotenvPath, []byte("PORT=38080\n"), 0644); err != nil {
		t.Fatalf("write dotenv: %v", err)
	}
	t.Setenv("ENV_FILE", dotenvPath)

	configuration, loadedPath, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loadedPath != dotenvPath {
		t.Errorf("loaded path = %q, want %q", loadedPath, dotenvPath)
	}
	if configuration.Addr != "localhost:38080" {
		t.Errorf("addr = %q, want localhost:38080", configuration.Addr)
	}
}

func TestLoadProcessEnvironmentWinsOverDotenvFile(t *testing.T) {
	clearProcessEnvironment(t)
	directory := t.TempDir()
	dotenvPath := filepath.Join(directory, ".env")
	if err := os.WriteFile(dotenvPath, []byte("PORT=39080\n"), 0644); err != nil {
		t.Fatalf("write dotenv: %v", err)
	}
	t.Setenv("ENV_FILE", dotenvPath)
	t.Setenv("PORT", "38080")

	configuration, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if configuration.Addr != "localhost:38080" {
		t.Errorf("addr = %q, want localhost:38080", configuration.Addr)
	}
}

func TestWriteDefaultFileRefusesExistingFile(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, ".env")
	if err := os.WriteFile(target, []byte("HOST=example\n"), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	if err := WriteDefaultFile(target); !errors.Is(err, ErrConfigFileExists) {
		t.Fatalf("error = %v, want ErrConfigFileExists", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(raw) != "HOST=example\n" {
		t.Fatalf("target content = %q, want untouched", string(raw))
	}
}

func TestWriteDefaultFileCreatesTemplate(t *testing.T) {
	target := filepath.Join(t.TempDir(), ".env")

	if err := WriteDefaultFile(target); err != nil {
		t.Fatalf("WriteDefaultFile: %v", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(raw) != DefaultTemplate() {
		t.Fatalf("template mismatch:\n%s", string(raw))
	}
	for _, key := range []string{"HOST=", "PORT=", "ADMIN_PORT=", "DB_PATH=", "CACHE_PATH=", "EVENTS_PATH=", "EVENTS_RETENTION_DAYS=", "LOG_LEVEL="} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("template misses %q", key)
		}
	}
	if strings.Contains(string(raw), "NO_AUTH=") {
		t.Error("template contains NO_AUTH, want it removed")
	}
	// DEV_UI_REDIRECT stays dev-only process state set by `make start`;
	// the prod template never generates it.
	if strings.Contains(string(raw), "DEV_UI_REDIRECT") {
		t.Error("template contains DEV_UI_REDIRECT, want dev-only process state excluded")
	}
}

func TestDotenvExampleMirrorsTemplate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", ".env.example"))
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	if string(raw) != DefaultTemplate() {
		t.Fatalf(".env.example drifts from DefaultTemplate:\n%s", string(raw))
	}
}
