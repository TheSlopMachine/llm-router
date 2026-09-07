// Package shared holds helpers common to all scripts.
package shared

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Proc is the JSON pidfile written by start and read by stop/status.
type Proc struct {
	Backend  int `json:"backend"`
	Frontend int `json:"frontend"`
	VitePort int `json:"vitePort"`
}

// RootDir returns the repo root: nearest ancestor of cwd containing go.mod
// alongside the internal/ tree. The go.mod check alone is not enough:
// scripts/ is a separate Go module nested inside the repo.
func RootDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for range 8 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if st, err := os.Stat(filepath.Join(dir, "internal")); err == nil && st.IsDir() {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("repo root not found (no go.mod with internal/ above %s)", cwd)
}

// DefaultPidFile is %TEMP%/llm-router-dev.pid (or $TMPDIR equivalent).
func DefaultPidFile() string { return filepath.Join(os.TempDir(), "llm-router-dev.pid") }

// DefaultBackendLog and DefaultFrontendLog sit next to the pidfile.
func DefaultBackendLog() string  { return filepath.Join(os.TempDir(), "llm-router-backend.log") }
func DefaultFrontendLog() string { return filepath.Join(os.TempDir(), "llm-router-frontend.log") }

// HomeLocal returns ~/.local/llm-router (or OS equivalent).
func HomeLocal() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("cannot resolve home directory")
	}
	return filepath.Join(home, ".local", "llm-router"), nil
}

// ReadPidFile parses path as JSON Proc. A legacy single-PID file is
// accepted as backend-only.
func ReadPidFile(path string) (*Proc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Proc
	if err := json.Unmarshal(raw, &p); err == nil && (p.Backend > 0 || p.Frontend > 0) {
		return &p, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("unrecognized pidfile content in %s", path)
	}
	return &Proc{Backend: n}, nil
}

// Stepf prints a "[>] ..." progress line.
func Stepf(format string, args ...any) { fmt.Printf("[>] "+format+"\n", args...) }

// OKf prints an "[OK] ..." success line.
func OKf(format string, args ...any) { fmt.Printf("[OK] "+format+"\n", args...) }

// Failf prints a "[FAIL] ..." line to stderr and exits non-zero.
func Failf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[FAIL] "+format+"\n", args...)
	os.Exit(1)
}
