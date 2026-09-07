package shared

import (
	"os"
	"path/filepath"
	"strings"
)

// IsNoSkip reports whether NO_SKIP is set to a truthy value (1/true/yes/on).
// When true, bun install and OpenAPI generation must not be skipped.
func IsNoSkip() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("NO_SKIP")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// Getenv returns the value of an env var or def when unset or blank.
func Getenv(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

// RequireEnv returns the value of an env var, failing when unset or blank.
func RequireEnv(name string) string {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		Failf("missing %s env", name)
	}
	return v
}

// DefaultDevDB returns ~/.local/llm-router/llm-router-dev.db (or OS equivalent).
func DefaultDevDB() string {
	if dir, err := HomeLocal(); err == nil {
		return filepath.Join(dir, "llm-router-dev.db")
	}
	return "~/.local/llm-router/llm-router-dev.db"
}

// DefaultDevKey returns ~/.local/llm-router/llm-router-dev.key (or OS equivalent).
func DefaultDevKey() string {
	if dir, err := HomeLocal(); err == nil {
		return filepath.Join(dir, "llm-router-dev.key")
	}
	return "~/.local/llm-router/llm-router-dev.key"
}

// EnvWithoutGowork returns the current environment with any GOWORK= entry
// removed. Project-root `go` commands (go work sync, go run, go vet/test/build)
// must not inherit GOWORK=off which is used only to run the scripts module itself.
func EnvWithoutGowork() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "GOWORK=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// EnvWith returns EnvWithoutGowork plus extra entries.
func EnvWith(extra []string) []string {
	if len(extra) == 0 {
		return EnvWithoutGowork()
	}
	out := EnvWithoutGowork()
	out = append(out, extra...)
	return out
}
