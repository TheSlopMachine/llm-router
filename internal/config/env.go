// Environment loading for llm-router: process variables with optional .env
// file support. Process variables win over the file. A missing file is not
// an error. .env.example mirrors DefaultTemplate.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Configuration defaults for environment loading.
const (
	DefaultHost                = "localhost"
	DefaultPort                = "8080"
	DefaultDatabasePath        = "llm-router.sqlite3"
	DefaultCachePath           = "llm-router.cache.sqlite3"
	DefaultEventsPath          = "llm-router.events.sqlite3"
	DefaultLogLevel            = "info"
	DefaultDotenvPath          = ".env"
	DefaultEventsRetentionDays = 3
)

// AdminHost pins the admin listener to loopback regardless of HOST.
// The loopback bind is the only protection on an authentication-free
// listener, so it is hardcoded, never configured.
const AdminHost = "127.0.0.1"

// Sentinel errors for environment loading. Match with errors.Is.
var (
	ErrInvalidPort            = errors.New("invalid port")
	ErrInvalidLogLevel        = errors.New("invalid log level")
	ErrInvalidEventsRetention = errors.New("invalid events retention")
	ErrConfigFileExists       = errors.New("configuration file exists")
	ErrMissingTargetPath      = errors.New("configuration target path is required")
)

// ResolveDotenvPath returns the dotenv path: ENV_FILE or .env.
func ResolveDotenvPath() string {
	if value := strings.TrimSpace(os.Getenv("ENV_FILE")); value != "" {
		return value
	}
	return DefaultDotenvPath
}

// Load reads the dotenv file (when present) and builds Config from the
// resulting process environment. It returns the loaded file path or empty
// when no file was present.
func Load() (*Config, string, error) {
	dotenvPath := ResolveDotenvPath()
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
	databasePath := environmentOrDefault("DB_PATH", "")
	if databasePath == "" {
		databasePath = environmentOrDefault("DEV_DB", DefaultDatabasePath)
	}
	cachePath := environmentOrDefault("CACHE_PATH", "")
	if cachePath == "" {
		cachePath = environmentOrDefault("DEV_CACHE", DefaultCachePath)
	}
	eventsPath := environmentOrDefault("EVENTS_PATH", "")
	if eventsPath == "" {
		eventsPath = environmentOrDefault("DEV_EVENTS", DefaultEventsPath)
	}
	retentionDays, err := parseEventsRetentionValue(environmentOrDefault("EVENTS_RETENTION_DAYS", ""))
	if err != nil {
		return nil, "", fmt.Errorf("invalid EVENTS_RETENTION_DAYS: %w", err)
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
DB_PATH=llm-router.sqlite3
CACHE_PATH=llm-router.cache.sqlite3
EVENTS_PATH=llm-router.events.sqlite3
EVENTS_RETENTION_DAYS=3
LOG_LEVEL=info
`
}

// WriteDefaultFile writes DefaultTemplate to target. It fails when the
// target exists or the path is blank.
func WriteDefaultFile(target string) error {
	cleaned := strings.TrimSpace(target)
	if cleaned == "" {
		return fmt.Errorf("configuration target path is required: %w", ErrMissingTargetPath)
	}
	if _, err := os.Stat(cleaned); err == nil {
		return fmt.Errorf("configuration file exists: %s: %w", cleaned, ErrConfigFileExists)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect configuration target %s: %w", cleaned, err)
	}
	if err := os.WriteFile(cleaned, []byte(DefaultTemplate()), 0644); err != nil {
		return fmt.Errorf("write configuration file %s: %w", cleaned, err)
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
