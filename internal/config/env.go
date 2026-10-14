// Environment loading for llm-router: process variables with optional .env
// file support. Process variables win over the file. A missing file is not
// an error. .env.example mirrors DefaultTemplate.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Configuration defaults for environment loading. Storage and dotenv
// defaults live in xdg.go (portable XDG forms).
const (
	DefaultHost                = "localhost"
	DefaultPort                = "8080"
	DefaultLogLevel            = "info"
	DefaultEventsRetentionDays = 3
	// DefaultLuaPoolBucketSize caps idle Lua states stored per plugin
	// bucket when LUA_POOL_BUCKET_SIZE is blank.
	DefaultLuaPoolBucketSize = 16
)

// AdminHost pins the admin listener to loopback regardless of HOST.
// The loopback bind is the only protection on an authentication-free
// listener, so it is hardcoded, never configured.
const AdminHost = "127.0.0.1"

// Sentinel errors for environment loading. Match with errors.Is.
var (
	ErrInvalidPort              = errors.New("invalid port")
	ErrInvalidLogLevel          = errors.New("invalid log level")
	ErrInvalidEventsRetention   = errors.New("invalid events retention")
	ErrInvalidLuaPoolBucketSize = errors.New("invalid lua pool bucket size")
	ErrConfigFileExists         = errors.New("configuration file exists")
	ErrMissingTargetPath        = errors.New("configuration target path is required")
)

// ResolveDotenvPath returns the dotenv path: ENV_FILE or the prod XDG default.
func ResolveDotenvPath() string {
	if value := strings.TrimSpace(os.Getenv("ENV_FILE")); value != "" {
		return value
	}
	return defaultDotenvPath()
}

// defaultDatabasePath returns the prod database default, honoring
// XDG_DATA_HOME with a portable home-relative fallback.
func defaultDatabasePath() string {
	if directory, err := DataApplicationDirectory(); err == nil {
		return filepath.Join(directory, "llm-router.sqlite3")
	}
	return DefaultDatabasePath
}

// defaultCachePath returns the prod cache default, honoring XDG_CACHE_HOME
// with a portable home-relative fallback.
func defaultCachePath() string {
	if directory, err := CacheApplicationDirectory(); err == nil {
		return filepath.Join(directory, "llm-router.cache.sqlite3")
	}
	return DefaultCachePath
}

// defaultEventsPath returns the prod events default, honoring
// XDG_STATE_HOME with a portable home-relative fallback.
func defaultEventsPath() string {
	if directory, err := StateApplicationDirectory(); err == nil {
		return filepath.Join(directory, "llm-router.events.sqlite3")
	}
	return DefaultEventsPath
}

// defaultDotenvPath returns the prod dotenv default, honoring
// XDG_CONFIG_HOME with a portable home-relative fallback.
func defaultDotenvPath() string {
	if directory, err := ConfigurationApplicationDirectory(); err == nil {
		return filepath.Join(directory, ".env")
	}
	return DefaultDotenvPath
}

// Load reads the dotenv file (when present) and builds Config from the
// resulting process environment. It returns the loaded file path or empty
// when no file was present.
func Load() (*Config, string, error) {
	dotenvPath, err := ExpandPath(ResolveDotenvPath())
	if err != nil {
		return nil, "", fmt.Errorf("invalid ENV_FILE: %w", err)
	}
	loadedPath, err := loadDotenvFile(dotenvPath)
	if err != nil {
		return nil, "", err
	}

	port, err := parsePortValue(environmentOrDefault("PORT", DefaultPort))
	if err != nil {
		return nil, "", fmt.Errorf("invalid PORT: %w", err)
	}
	// WEB_PORT, API_PORT and NO_AUTH are legacy names. They are ignored:
	// the single listener reads PORT only, and the admin listener reads
	// ADMIN_PORT only.
	logLevel, err := parseLogLevelValue(environmentOrDefault("LOG_LEVEL", DefaultLogLevel))
	if err != nil {
		return nil, "", fmt.Errorf("invalid LOG_LEVEL: %w", err)
	}
	adminAddr, err := parseAdminAddr(port)
	if err != nil {
		return nil, "", err
	}

	host := environmentOrDefault("HOST", DefaultHost)
	databasePath := environmentOrDefault("DB_PATH", defaultDatabasePath())
	databasePath, err = ExpandPath(databasePath)
	if err != nil {
		return nil, "", fmt.Errorf("invalid DB_PATH: %w", err)
	}
	if err := EnsureParentDirectory(databasePath); err != nil {
		return nil, "", fmt.Errorf("invalid DB_PATH: %w", err)
	}
	cachePath := environmentOrDefault("CACHE_PATH", defaultCachePath())
	cachePath, err = ExpandPath(cachePath)
	if err != nil {
		return nil, "", fmt.Errorf("invalid CACHE_PATH: %w", err)
	}
	if err := EnsureParentDirectory(cachePath); err != nil {
		return nil, "", fmt.Errorf("invalid CACHE_PATH: %w", err)
	}
	eventsPath := environmentOrDefault("EVENTS_PATH", defaultEventsPath())
	eventsPath, err = ExpandPath(eventsPath)
	if err != nil {
		return nil, "", fmt.Errorf("invalid EVENTS_PATH: %w", err)
	}
	if err := EnsureParentDirectory(eventsPath); err != nil {
		return nil, "", fmt.Errorf("invalid EVENTS_PATH: %w", err)
	}
	retentionDays, err := parseEventsRetentionValue(environmentOrDefault("EVENTS_RETENTION_DAYS", ""))
	if err != nil {
		return nil, "", fmt.Errorf("invalid EVENTS_RETENTION_DAYS: %w", err)
	}
	poolBucketSize, err := parseLuaPoolBucketSizeValue(environmentOrDefault("LUA_POOL_BUCKET_SIZE", ""))
	if err != nil {
		return nil, "", fmt.Errorf("invalid LUA_POOL_BUCKET_SIZE: %w", err)
	}

	configuration := &Config{
		Addr:                net.JoinHostPort(host, port.String()),
		AdminAddr:           adminAddr,
		DBPath:              databasePath,
		CachePath:           cachePath,
		EventsPath:          eventsPath,
		EventsRetentionDays: retentionDays,
		LogLevel:            logLevel,
		DevUIRedirect:       strings.TrimSpace(os.Getenv("DEV_UI_REDIRECT")),
		LuaPoolBucketSize:   poolBucketSize,
	}
	if err := configuration.Validate(); err != nil {
		return nil, "", err
	}
	return configuration, loadedPath, nil
}

// DefaultTemplate renders the default .env content. .env.example mirrors it.
func DefaultTemplate() string {
	return `# llm-router configuration file
HOST=localhost
PORT=8080
ADMIN_PORT=
DB_PATH=` + DefaultDatabasePath + `
CACHE_PATH=` + DefaultCachePath + `
EVENTS_PATH=` + DefaultEventsPath + `
EVENTS_RETENTION_DAYS=3
LOG_LEVEL=info
LUA_POOL_BUCKET_SIZE=16
`
}

// WriteDefaultFile writes DefaultTemplate to target. It fails when the
// target exists or the path is blank. Managed configuration parents are
// created; custom parents must exist.
func WriteDefaultFile(target string) error {
	cleaned := strings.TrimSpace(target)
	if cleaned == "" {
		return fmt.Errorf("configuration target path is required: %w", ErrMissingTargetPath)
	}
	expandedTarget, err := ExpandPath(cleaned)
	if err != nil {
		return fmt.Errorf("invalid configuration target %s: %w", cleaned, err)
	}
	if err := EnsureParentDirectory(expandedTarget); err != nil {
		return fmt.Errorf("invalid configuration target %s: %w", cleaned, err)
	}
	if _, err := os.Stat(expandedTarget); err == nil {
		return fmt.Errorf("configuration file exists: %s: %w", expandedTarget, ErrConfigFileExists)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect configuration target %s: %w", expandedTarget, err)
	}
	if err := os.WriteFile(expandedTarget, []byte(DefaultTemplate()), 0644); err != nil {
		return fmt.Errorf("write configuration file %s: %w", expandedTarget, err)
	}
	return nil
}

// loadDotenvFile loads path into the process environment without overriding
// existing variables. It returns the path when loaded, empty when absent.
func loadDotenvFile(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("inspect dotenv file %s: %w", path, err)
	}
	if err := godotenv.Load(path); err != nil {
		return "", fmt.Errorf("load dotenv file %s: %w", path, err)
	}
	return path, nil
}

// environmentOrDefault returns the trimmed variable value or fallback when
// blank.
func environmentOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

// parsePortValue parses a TCP port number (1-65535).
func parsePortValue(raw string) (Port, error) {
	var port Port
	if err := port.Set(raw); err != nil {
		return 0, fmt.Errorf("%q: %w", raw, ErrInvalidPort)
	}
	return port, nil
}

// parseLogLevelValue normalizes a log level name (warning maps to warn).
func parseLogLevelValue(raw string) (LogLevel, error) {
	var level LogLevel
	if err := level.Set(raw); err != nil {
		return "", fmt.Errorf("%q: %w", raw, ErrInvalidLogLevel)
	}
	return level, nil
}

// parseEventsRetentionValue parses the events retention in days. Blank
// returns the default. Values below 1 fail: zero never disables pruning.
func parseEventsRetentionValue(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return DefaultEventsRetentionDays, nil
	}
	days, err := strconv.Atoi(trimmed)
	if err != nil || days < 1 {
		return 0, fmt.Errorf("%q: %w", raw, ErrInvalidEventsRetention)
	}
	return days, nil
}

// parseLuaPoolBucketSizeValue parses the per-bucket idle Lua state limit.
// Blank returns the default. Zero disables pooling. Negative values fail.
func parseLuaPoolBucketSizeValue(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return DefaultLuaPoolBucketSize, nil
	}
	size, err := strconv.Atoi(trimmed)
	if err != nil || size < 0 {
		return 0, fmt.Errorf("%q: %w", raw, ErrInvalidLuaPoolBucketSize)
	}
	return size, nil
}

// parseAdminAddr resolves the admin listener address from ADMIN_PORT.
// Empty disables the listener. A set port binds AdminHost (loopback),
// never HOST, and must differ from the public port.
func parseAdminAddr(publicPort Port) (string, error) {
	raw := strings.TrimSpace(os.Getenv("ADMIN_PORT"))
	if raw == "" {
		return "", nil
	}
	adminPort, err := parsePortValue(raw)
	if err != nil {
		return "", fmt.Errorf("invalid ADMIN_PORT: %w", err)
	}
	if adminPort == publicPort {
		return "", fmt.Errorf("ADMIN_PORT must differ from PORT (both %s)", adminPort.String())
	}
	return net.JoinHostPort(AdminHost, adminPort.String()), nil
}
