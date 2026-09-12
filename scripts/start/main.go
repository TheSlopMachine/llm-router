package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

const devBackendWebPort = "38473" // dev-only internal backend --web; never user-facing

var exeSuffix = map[bool]string{true: ".exe", false: ""}[runtime.GOOS == "windows"]

// start launches the dev backend (built binary) and frontend (vite).
// Project initialization lives in `make init`; configuration comes from env only.
func main() {
	host := shared.Getenv("HOST", "localhost")
	webPort := shared.Getenv("WEB_PORT", "8080")
	apiPort := shared.Getenv("API_PORT", "8081")
	logLevel := shared.NormalizeLogLevel(shared.Getenv("LOG_LEVEL", "info"))
	dbPath := shared.RequireEnv("DEV_DB")
	keyPath := shared.RequireEnv("DEV_KEY")
	pidFile := shared.Getenv("PID_FILE", shared.DefaultPidFile())

	root, err := shared.RootDir()
	if err != nil {
		shared.Failf("%v", err)
	}

	// Refuse to clobber an already-running dev server. Spawning a second
	// backend/frontend pair here would silently overwrite the pidfile,
	// orphaning the first pair — no `make stop` could ever reach them again.
	if p, err := shared.ReadPidFile(pidFile); err == nil {
		if (p.Backend > 0 && shared.Alive(p.Backend)) || (p.Frontend > 0 && shared.Alive(p.Frontend)) {
			shared.Failf("already running (see `make status`); run `make stop` or `make restart` first")
		}
		// Stale pidfile pointing at dead processes: safe to remove and continue.
		_ = os.Remove(pidFile)
	}

	// Fail fast when a port is held by a process outside this pidfile.
	// The actual bind at startup stays the source of truth.
	for _, port := range []int{mustAtoi(webPort), mustAtoi(apiPort), mustAtoi(devBackendWebPort)} {
		if free, holder := shared.PortStatus(port); !free {
			shared.Failf("port %d already in use by %s", port, holder)
		}
	}

	// Ensure parent dirs for db and pidfile.
	for _, p := range []string{dbPath, pidFile, shared.DefaultBackendLog(), shared.DefaultFrontendLog()} {
		dir := filepath.Dir(p)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				shared.Failf("create dir %s: %v", dir, err)
			}
		}
	}

	backendLog := shared.DefaultBackendLog()
	frontendLog := shared.DefaultFrontendLog()

	shared.Stepf("Building backend...")

	// Build to a temp binary and spawn it directly: `go run` would put its
	// own wrapper PID in the pidfile, and the compiled child it execs can
	// outlive the wrapper (reparented to init) while holding the ports —
	// `make stop` then never reaches the real server.
	binPath := filepath.Join(os.TempDir(), "llm-router-dev-backend"+exeSuffix)
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		shared.Failf("backend build: %v\n%s", err, out)
	}

	shared.Stepf("Starting backend and frontend (vite)...")

	backendPID, err := shared.SpawnDetached(root, backendLog, binPath, host,
		"--web", devBackendWebPort, "--api", apiPort, "--db", dbPath, "--testing-key", keyPath,
		"--log-level", logLevel,
		"--dev-ui-redirect", fmt.Sprintf("http://%s:%s", host, webPort))
	if err != nil {
		shared.Failf("spawn backend: %v", err)
	}

	webDir := filepath.Join(root, "web")
	env := []string{
		"VITE_BACKEND_HOST=" + host,
		"VITE_BACKEND_PORT=" + devBackendWebPort,
	}
	frontendPID, err := shared.SpawnDetachedEnv(webDir, frontendLog, env, "bun", "run", "dev", "--", "--host", host, "--port", webPort)
	if err != nil {
		// Attempt to stop the backend we just started.
		_ = shared.Terminate(backendPID)
		shared.Failf("spawn frontend: %v", err)
	}

	// Group both processes so a later stop reaches descendants too.
	// Registration failure only loses the descendant guarantee.
	if err := shared.RegisterSession(pidFile, backendPID, frontendPID); err != nil {
		fmt.Fprintf(os.Stderr, "[WARN] process group registration failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "[WARN] stop falls back to best-effort termination")
	}

	proc := shared.Proc{Backend: backendPID, Frontend: frontendPID, VitePort: mustAtoi(webPort)}
	raw, _ := json.MarshalIndent(proc, "", "  ")
	if err := os.WriteFile(pidFile, append(raw, '\n'), 0644); err != nil {
		shared.Failf("write pidfile: %v", err)
	}

	// Give the OS a moment to report a very early exit (e.g. missing binary)
	// without doing a health poll. This is not a readiness check.
	time.Sleep(200 * time.Millisecond)

	fmt.Printf("[OK] Started backend PID %d, frontend PID %d\n", backendPID, frontendPID)
	fmt.Printf("Dashboard (dev): http://%s:%s\n", host, webPort)
	fmt.Printf("API: http://%s:%s/v1\n", host, apiPort)
	if raw, err := os.ReadFile(keyPath); err == nil {
		if s := string(raw); len(s) > 0 {
			// Trim newline, print first line only.
			if idx := indexNewline(s); idx >= 0 {
				s = s[:idx]
			}
			fmt.Printf("API Key: %s\n", s)
		}
	}
	fmt.Printf("Backend log: %s\n", backendLog)
	fmt.Printf("Frontend log: %s\n", frontendLog)
	fmt.Printf("Pidfile: %s\n", pidFile)
}

func mustAtoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			shared.Failf("invalid port %q", s)
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func indexNewline(s string) int {
	for i, c := range s {
		if c == '\n' || c == '\r' {
			return i
		}
	}
	return -1
}
