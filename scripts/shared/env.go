package shared

import (
	"os"
	"path/filepath"
	"strings"
)

// IsNoSkip reports whether NO_SKIP is set to a truthy value (1/true/yes/on).
// When true, bun install and OpenAPI generation must not be skipped.
func IsNoSkip() bool {
	return IsWriteMode("NO_SKIP")
}

// IsWriteMode reports whether the named env var is truthy (1/true/yes/on).
func IsWriteMode(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

// Getenv returns the value of an env var or def when unset or blank.
func Getenv(name, def string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return def
}

// RequireEnv returns the value of an env var, failing when unset or blank.
func RequireEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		Failf("missing %s env", name)
	}
	return value
}

// NormalizeLogLevel lowercases and trims a log level, failing on values
// outside debug/info/warn/warning/error — the set internal/config accepts.
func NormalizeLogLevel(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "debug", "info", "warn", "warning", "error":
		return normalized
	default:
		Failf("invalid LOG_LEVEL %q: must be one of debug, info, warn, error", value)
		return ""
	}
}

// DefaultDevDB returns ~/.local/llm-router/llm-router-dev.sqlite3 (or OS equivalent).
func DefaultDevDB() string {
	if dir, err := HomeLocal(); err == nil {
		return filepath.Join(dir, "llm-router-dev.sqlite3")
	}
	return "~/.local/llm-router/llm-router-dev.sqlite3"
}

// DefaultDevCache returns ~/.local/llm-router/llm-router-dev.cache.sqlite3 (or OS equivalent).
func DefaultDevCache() string {
	if dir, err := HomeLocal(); err == nil {
		return filepath.Join(dir, "llm-router-dev.cache.sqlite3")
	}
	return "~/.local/llm-router/llm-router-dev.cache.sqlite3"
}

// EnvWithoutGowork returns the current environment with any GOWORK= entry
// removed. Project-root `go` commands (go work sync, go run, go vet/test/build)
// must not inherit GOWORK=off which is used only to run the scripts module itself.
func EnvWithoutGowork() []string {
	env := os.Environ()
	output := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, "GOWORK=") {
			continue
		}
		output = append(output, entry)
	}
	return output
}

// EnvWith returns EnvWithoutGowork plus extra entries. Extra entries win
// over inherited ones with the same key.
func EnvWith(extra []string) []string {
	if len(extra) == 0 {
		return EnvWithoutGowork()
	}
	combined := append(EnvWithoutGowork(), extra...)
	positionByKey := make(map[string]int, len(combined))
	output := make([]string, 0, len(combined))
	for _, entry := range combined {
		key := entry
		if keyPart, _, found := strings.Cut(entry, "="); found {
			key = keyPart
		}
		if position, exists := positionByKey[key]; exists {
			output[position] = entry
			continue
		}
		positionByKey[key] = len(output)
		output = append(output, entry)
	}
	return output
}
