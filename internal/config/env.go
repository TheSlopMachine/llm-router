// Environment loading for llm-router: process variables with optional .env
// file support. Process variables win over the file. A missing file is not
// an error. .env.example mirrors DefaultTemplate.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// Configuration defaults for environment loading.
const (
	DefaultHost         = "localhost"
	DefaultPort         = "8080"
	DefaultDatabasePath = "llm-router.sqlite3"
	DefaultCachePath    = "llm-router.cache.sqlite3"
	DefaultLogLevel     = "info"
	DefaultDotenvPath   = ".env"
)

// Sentinel errors for environment loading. Match with errors.Is.
var (
	ErrInvalidPort       = errors.New("invalid port")
	ErrInvalidLogLevel   = errors.New("invalid log level")
	ErrInvalidNoAuth     = errors.New("invalid NO_AUTH value")
	ErrConfigFileExists  = errors.New("configuration file exists")
	ErrMissingTargetPath = errors.New("configuration target path is required")
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
	// WEB_PORT and API_PORT are legacy names from the split-listener era.
	// They are ignored: the single listener reads PORT only.
	logLevel, err := parseLogLevelValue(environmentOrDefault("LOG_LEVEL", DefaultLogLevel))
	if err != nil {
		return nil, "", fmt.Errorf("invalid LOG_LEVEL: %w", err)
	}
	noAuth, err := parseNoAuthValue(strings.TrimSpace(os.Getenv("NO_AUTH")))
	if err != nil {
		return nil, "", fmt.Errorf("invalid NO_AUTH: %w", err)
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

	configuration := &Config{
		Addr:          net.JoinHostPort(host, port.String()),
		DBPath:        databasePath,
		CachePath:     cachePath,
		LogLevel:      logLevel,
		NoAuth:        noAuth,
		DevUIRedirect: strings.TrimSpace(os.Getenv("DEV_UI_REDIRECT")),
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
DB_PATH=llm-router.sqlite3
CACHE_PATH=llm-router.cache.sqlite3
LOG_LEVEL=info
NO_AUTH=0
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

// parseNoAuthValue parses NO_AUTH: truthy 1/true/yes/on, falsy blank,
// 0/false/no/off. Unknown values fail closed.
func parseNoAuthValue(raw string) (bool, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "", "0", "false", "no", "off":
		return false, nil
	case "1", "true", "yes", "on":
		return true, nil
	default:
		return false, fmt.Errorf("%q: %w", raw, ErrInvalidNoAuth)
	}
}
