// XDG directory layout for the scripts module: portable defaults plus
// managed directory creation. Bases honor the XDG variable first and fall
// back to a home-relative directory on every OS.
//
// Mirror: internal/config/xdg.go carries the same semantics for the server
// module. The two files stay in sync; stability tests in both modules lock
// the behavior.
package shared

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Portable dotenv and storage defaults (unexpanded). Scripts expand them
// through ExpandPath, so every OS resolves the same text.
const (
	DefaultDatabasePath = "~/.local/share/llm-router/llm-router.sqlite3"
	DefaultCachePath    = "~/.cache/llm-router/llm-router.cache.sqlite3"
	DefaultEventsPath   = "~/.local/state/llm-router/llm-router.events.sqlite3"
	DefaultDotenvPath   = "~/.config/llm-router/.env"

	DefaultDevDatabasePath = "~/.local/share/llm-router/llm-router.dev.sqlite3"
	DefaultDevCachePath    = "~/.cache/llm-router/llm-router.cache.dev.sqlite3"
	DefaultDevEventsPath   = "~/.local/state/llm-router/llm-router.events.dev.sqlite3"
	DefaultDevDotenvPath   = "~/.config/llm-router/.env.dev"

	DefaultDevPidFileName     = "llm-router.dev.pid"
	DefaultDevFrontendLogName = "llm-router-frontend.dev.log"
)

// ApplicationDirectoryName is the single directory entry under each XDG
// base that llm-router owns.
const ApplicationDirectoryName = "llm-router"

// Sentinel errors for managed directory creation. Match with errors.Is.
var ErrParentMissing = errors.New("parent directory missing")

// ConfigurationApplicationDirectory returns the expanded configuration
// application directory ($XDG_CONFIG_HOME/llm-router else ~/.config).
func ConfigurationApplicationDirectory() (string, error) {
	base, err := baseDirectory("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return "", err
	}
	return filepath.Join(base, ApplicationDirectoryName), nil
}

// DataApplicationDirectory returns the expanded data application directory
// ($XDG_DATA_HOME/llm-router else ~/.local/share).
func DataApplicationDirectory() (string, error) {
	base, err := baseDirectory("XDG_DATA_HOME", filepath.Join(".local", "share"))
	if err != nil {
		return "", err
	}
	return filepath.Join(base, ApplicationDirectoryName), nil
}

// StateApplicationDirectory returns the expanded state application
// directory ($XDG_STATE_HOME/llm-router else ~/.local/state).
func StateApplicationDirectory() (string, error) {
	base, err := baseDirectory("XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return "", err
	}
	return filepath.Join(base, ApplicationDirectoryName), nil
}

// CacheApplicationDirectory returns the expanded cache application
// directory ($XDG_CACHE_HOME/llm-router else ~/.cache).
func CacheApplicationDirectory() (string, error) {
	base, err := baseDirectory("XDG_CACHE_HOME", ".cache")
	if err != nil {
		return "", err
	}
	return filepath.Join(base, ApplicationDirectoryName), nil
}

// DefaultDevDatabase returns the expanded dev database path.
func DefaultDevDatabase() string {
	if directory, err := DataApplicationDirectory(); err == nil {
		return filepath.Join(directory, "llm-router.dev.sqlite3")
	}
	return DefaultDevDatabasePath
}

// DefaultDevCache returns the expanded dev cache path.
func DefaultDevCache() string {
	if directory, err := CacheApplicationDirectory(); err == nil {
		return filepath.Join(directory, "llm-router.cache.dev.sqlite3")
	}
	return DefaultDevCachePath
}

// DefaultDevEvents returns the expanded dev events path.
func DefaultDevEvents() string {
	if directory, err := StateApplicationDirectory(); err == nil {
		return filepath.Join(directory, "llm-router.events.dev.sqlite3")
	}
	return DefaultDevEventsPath
}

// DefaultDevDotenv returns the expanded dev dotenv path.
func DefaultDevDotenv() string {
	if directory, err := ConfigurationApplicationDirectory(); err == nil {
		return filepath.Join(directory, ".env.dev")
	}
	return DefaultDevDotenvPath
}

// IsManagedPath reports whether path sits inside one of the four managed
// application directories. Both the XDG-resolved and the home-fallback
// locations count as managed, so generated home-relative templates keep
// working when an XDG override points elsewhere. Unresolvable bases report
// false (fail closed).
func IsManagedPath(path string) bool {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "" || cleaned == "." {
		return false
	}
	for _, directory := range managedDirectories() {
		if pathWithinDirectory(cleaned, directory) {
			return true
		}
	}
	return false
}

// managedDirectories returns the resolved application directories plus the
// home-fallback ones. Callers ignore unresolvable entries.
func managedDirectories() []string {
	directories := make([]string, 0, 8)
	if directory, err := ConfigurationApplicationDirectory(); err == nil {
		directories = append(directories, directory)
	}
	if directory, err := DataApplicationDirectory(); err == nil {
		directories = append(directories, directory)
	}
	if directory, err := StateApplicationDirectory(); err == nil {
		directories = append(directories, directory)
	}
	if directory, err := CacheApplicationDirectory(); err == nil {
		directories = append(directories, directory)
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		directories = append(directories,
			filepath.Join(home, ".config", ApplicationDirectoryName),
			filepath.Join(home, ".local", "share", ApplicationDirectoryName),
			filepath.Join(home, ".local", "state", ApplicationDirectoryName),
			filepath.Join(home, ".cache", ApplicationDirectoryName),
		)
	}
	return directories
}

// EnsureParentDirectory creates the parent of path when missing and managed,
// requires it when missing and unmanaged, and does nothing when present.
// Relative parents (".") always exist. Custom directories never gain
// directories implicitly: a missing custom parent returns ErrParentMissing.
func EnsureParentDirectory(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}
	parent := filepath.Dir(filepath.Clean(trimmed))
	if parent == "." || parent == "" {
		return nil
	}
	if info, err := os.Stat(parent); err == nil {
		if info.IsDir() {
			return nil
		}
		return fmt.Errorf("parent path %s is not a directory: %w", parent, ErrParentMissing)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect parent directory %s: %w", parent, err)
	}
	if !IsManagedPath(parent) && !IsManagedPath(trimmed) {
		return fmt.Errorf("parent directory %s does not exist: %w", parent, ErrParentMissing)
	}
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("create parent directory %s: %w", parent, err)
	}
	return nil
}

// baseDirectory resolves one XDG base: the variable value when set, else the
// home-relative fallback suffix.
func baseDirectory(variableName, fallbackSuffix string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(variableName)); value != "" {
		expanded, err := ExpandPath(value)
		if err != nil {
			return "", fmt.Errorf("invalid %s: %w", variableName, err)
		}
		if strings.TrimSpace(expanded) == "" {
			return "", fmt.Errorf("invalid %s: %w", variableName, ErrHomeDirectoryUnresolvable)
		}
		return expanded, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("resolve home directory for %s: %w", variableName, ErrHomeDirectoryUnresolvable)
	}
	return filepath.Join(home, fallbackSuffix), nil
}

// pathWithinDirectory reports whether candidate equals directory or lives
// below it. Comparison is case-insensitive to cover Windows paths.
func pathWithinDirectory(candidate, directory string) bool {
	cleanedCandidate := filepath.Clean(candidate)
	cleanedDirectory := filepath.Clean(directory)
	if strings.EqualFold(cleanedCandidate, cleanedDirectory) {
		return true
	}
	prefix := cleanedDirectory
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if len(cleanedCandidate) <= len(prefix) {
		return false
	}
	return strings.EqualFold(cleanedCandidate[:len(prefix)], prefix)
}
