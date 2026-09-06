package shared

import (
	"os"
	"strings"
)

// IsNoSkip reports whether NO_SKIP is set to a truthy value (1/true/yes/on).
// When true, bun install and OpenAPI generation must not be skipped.
func IsNoSkip() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("NO_SKIP")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
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
