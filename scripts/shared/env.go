package shared

import (
	"os"
	"strings"
)

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
