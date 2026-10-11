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
	Backend      int    `json:"backend"`
	Frontend     int    `json:"frontend"`
	VitePort     int    `json:"vitePort"`
	BackendPath  string `json:"backendPath,omitempty"`
	FrontendPath string `json:"frontendPath,omitempty"`
}

// RootDir returns the repository root: nearest ancestor of workdir containing go.mod
// alongside the internal/ tree. The go.mod check alone is not enough:
// scripts/ is a separate Go module nested inside the repository.
func RootDir() (string, error) {
	workdir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	ancestorDirectory := workdir
	for range 8 {
		if _, err := os.Stat(filepath.Join(ancestorDirectory, "go.mod")); err == nil {
			if fileInfo, err := os.Stat(filepath.Join(ancestorDirectory, "internal")); err == nil && fileInfo.IsDir() {
				return ancestorDirectory, nil
			}
		}
		parent := filepath.Dir(ancestorDirectory)
		if parent == ancestorDirectory {
			break
		}
		ancestorDirectory = parent
	}
	return "", fmt.Errorf("repository root not found (no go.mod with internal/ above %s)", workdir)
}

// DefaultPidFile is %TEMP%/llm-router-dev.pid (or $TMPDIR equivalent).
func DefaultPidFile() string { return filepath.Join(os.TempDir(), "llm-router-dev.pid") }

// StartParamsFile lives next to the pidfile and records the parameters of the
// last start, so restart relaunches with the same configuration.
type StartParams struct {
	DevDB    string `json:"dev_db"`
	NoAuth   bool   `json:"no_auth"`
	Host     string `json:"host"`
	WebPort  string `json:"web_port"`
	APIPort  string `json:"api_port"`
	LogLevel string `json:"log_level"`
}

// ParamsFileFor returns the params file path next to the given pidfile.
func ParamsFileFor(pidFile string) string {
	return filepath.Join(filepath.Dir(pidFile), "llm-router-dev.params.json")
}

// WriteStartParams persists the start parameters.
func WriteStartParams(pidFile string, params StartParams) error {
	raw, err := json.MarshalIndent(params, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ParamsFileFor(pidFile), append(raw, '\n'), 0644)
}

// ReadStartParams reads the recorded start parameters.
func ReadStartParams(pidFile string) (*StartParams, error) {
	raw, err := os.ReadFile(ParamsFileFor(pidFile))
	if err != nil {
		return nil, err
	}
	var params StartParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	return &params, nil
}

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
	var process Proc
	if err := json.Unmarshal(raw, &process); err == nil && (process.Backend > 0 || process.Frontend > 0) {
		return &process, nil
	}
	number, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || number <= 0 {
		return nil, fmt.Errorf("unrecognized pidfile content in %s", path)
	}
	return &Proc{Backend: number}, nil
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
