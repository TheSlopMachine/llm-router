package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

var exeSuffix = map[bool]string{true: ".exe", false: ""}[runtime.GOOS == "windows"]

// start launches the dev backend (built binary) and frontend (vite).
// Project initialization lives in `make init`; configuration comes from env only.
func main() {
	host := shared.Getenv("HOST", "localhost")
	port := shared.Getenv("PORT", "38080")
	logLevel := shared.NormalizeLogLevel(shared.Getenv("LOG_LEVEL", "info"))
	databasePath := shared.GetenvPath("DEV_DB", "")
	if databasePath == "" {
		databasePath = shared.GetenvPath("DB_PATH", shared.DefaultDevDB())
	}
	cachePath := shared.GetenvPath("DEV_CACHE", "")
	if cachePath == "" {
		cachePath = shared.GetenvPath("CACHE_PATH", shared.DefaultDevCache())
	}
	eventsPath := shared.GetenvPath("DEV_EVENTS", "")
	if eventsPath == "" {
		eventsPath = shared.GetenvPath("EVENTS_PATH", shared.DefaultDevEvents())
	}
	retentionDays := shared.Getenv("EVENTS_RETENTION_DAYS", "3")
	adminPort := shared.Getenv("ADMIN_PORT", shared.DevAdminPort)
	pidFile := shared.GetenvPath("PID_FILE", shared.DefaultPidFile())

	// SCOPE selects the start set: all (default) or backend only. The
	// backend-only path rebuilds and respawns the backend while a live
	// frontend keeps serving (vite hot-reloads the SPA itself).
	backendOnly := false
	switch scope := strings.TrimSpace(shared.Getenv("SCOPE", "all")); scope {
	case "", "all":
	case "backend":
		backendOnly = true
	default:
		shared.Failf("invalid SCOPE %q: want all or backend", scope)
	}

	root, err := shared.RootDir()
	if err != nil {
		shared.Failf("%v", err)
	}

	// keptFrontend carries the live frontend across a backend-only start.
	kept := shared.Proc{}
	// Refuse to clobber an already-running dev server. Spawning a second
	// backend/frontend pair here would silently overwrite the pidfile,
	// orphaning the first pair — no `make stop` could ever reach them again.
	if recorded, err := shared.ReadPidFile(pidFile); err == nil {
		if shared.AliveMatches(recorded.Backend, recorded.BackendPath) {
			shared.Failf("already running (see `make status`); run `make stop` or `make restart` first")
		}
		if backendOnly {
			if shared.AliveMatches(recorded.Frontend, recorded.FrontendPath) {
				kept.Frontend = recorded.Frontend
				kept.FrontendPath = recorded.FrontendPath
				kept.VitePort = recorded.VitePort
			}
		} else {
			if shared.AliveMatches(recorded.Frontend, recorded.FrontendPath) {
				shared.Failf("already running (see `make status`); run `make stop` or `make restart` first")
			}
			// Stale pidfile pointing at dead processes: safe to remove and continue.
			_ = os.Remove(pidFile)
		}
	}

	// Fail fast when a port is held by a process outside this pidfile.
	// The actual bind at startup stays the source of truth. The vite
	// port stays out of the backend-only check: the kept frontend
	// legitimately holds it.
	if port == shared.DevBackendPort {
		shared.Failf("PORT %s collides with the fixed dev backend port %s", port, shared.DevBackendPort)
	}
	// The new backend redirects to the running vite instance when one is
	// kept, not to a possibly different PORT value.
	vitePort := port
	if kept.Frontend > 0 && kept.VitePort > 0 {
		vitePort = fmt.Sprintf("%d", kept.VitePort)
	}
	ports := []int{mustAtoi(shared.DevBackendPort)}
	if !backendOnly {
		ports = append(ports, mustAtoi(port))
	}
	if adminPort != "" {
		if adminPort == port || adminPort == shared.DevBackendPort {
			shared.Failf("ADMIN_PORT %s collides with the dev ports", adminPort)
		}
		ports = append(ports, mustAtoi(adminPort))
	}
	for _, portNumber := range ports {
		if free, holder := shared.PortStatus(portNumber); !free {
			shared.Failf("port %d already in use by %s", portNumber, holder)
		}
	}

	// Ensure parent dirs for db and pidfile.
	for _, path := range []string{databasePath, cachePath, eventsPath, pidFile, shared.DefaultFrontendLog()} {
		parent := filepath.Dir(path)
		if parent != "." && parent != "" {
			if err := os.MkdirAll(parent, 0755); err != nil {
				shared.Failf("create dir %s: %v", parent, err)
			}
		}
	}

	frontendLog := shared.DefaultFrontendLog()

	shared.Stepf("Building backend...")

	// Build to a temp binary and spawn it directly: `go run` would put its
	// own wrapper PID in the pidfile, and the compiled child it execs can
	// outlive the wrapper (reparented to init) while holding the ports —
	// `make stop` then never reaches the real server.
	binPath := filepath.Join(os.TempDir(), "llm-router-dev-backend"+exeSuffix)
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = root
	// Project-root builds run in the workspace: GOWORK=off applies to the
	// scripts module only and must not leak into this command.
	build.Env = shared.EnvWithoutGowork()
	if output, err := build.CombinedOutput(); err != nil {
		shared.Failf("backend build: %v\n%s", err, output)
	}

	frontendPID, frontendPath := 0, ""
	if backendOnly && kept.Frontend > 0 {
		shared.Stepf("Starting backend (frontend kept)...")
	} else {
		shared.Stepf("Starting backend and frontend (vite)...")
	}

	backendEnvironment := []string{
		"HOST=" + host,
		"PORT=" + shared.DevBackendPort,
		"ADMIN_PORT=" + adminPort,
		"DB_PATH=" + databasePath,
		"CACHE_PATH=" + cachePath,
		"EVENTS_PATH=" + eventsPath,
		"EVENTS_RETENTION_DAYS=" + retentionDays,
		"LOG_LEVEL=" + logLevel,
		"DEV_UI_REDIRECT=" + fmt.Sprintf("http://%s:%s", host, vitePort),
	}
	backendPID, err := shared.SpawnDetachedEnvNoLog(root, backendEnvironment, binPath)
	if err != nil {
		shared.Failf("spawn backend: %v", err)
	}

	frontendPID, frontendPath = kept.Frontend, kept.FrontendPath
	if !backendOnly {
		webDir := filepath.Join(root, "web")
		// A broken ::1 stack binds an unreachable socket when given
		// "localhost" (Node resolves it to ::1 first). Pin IPv4 loopback for
		// the default host only; explicit HOST values pass through untouched.
		// Covers both the listen socket and the vite → backend proxy target.
		viteHost, backendHost := host, host
		if host == "localhost" {
			viteHost, backendHost = "127.0.0.1", "127.0.0.1"
		}
		env := []string{
			"VITE_BACKEND_HOST=" + backendHost,
			"VITE_BACKEND_PORT=" + shared.DevBackendPort,
		}
		frontendPID, err = shared.SpawnDetachedEnv(webDir, frontendLog, env, "bun", "run", "dev", "--", "--host", viteHost, "--port", port)
		if err != nil {
			// Attempt to stop the backend we just started.
			if runtime.GOOS == "windows" {
				_ = shared.ForceKillTargets(pidFile, shared.KillTarget{PID: backendPID, ExpectedPath: binPath})
			} else {
				_ = shared.Terminate(backendPID)
			}
			shared.Failf("spawn frontend: %v", err)
		}
		frontendPath = snapshotPath(frontendPID, resolveBun())
	}

	// Group both processes so a later stop reaches descendants too.
	// Registration failure only loses the descendant guarantee.
	sessionPIDs := []int{backendPID}
	if frontendPID > 0 {
		sessionPIDs = append(sessionPIDs, frontendPID)
	}
	if err := shared.RegisterSession(pidFile, sessionPIDs...); err != nil {
		fmt.Fprintf(os.Stderr, "[WARN] process group registration failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "[WARN] stop falls back to best-effort termination")
	}

	process := shared.Proc{
		Backend:      backendPID,
		Frontend:     frontendPID,
		VitePort:     mustAtoi(vitePort),
		BackendPath:  snapshotPath(backendPID, binPath),
		FrontendPath: frontendPath,
	}
	raw, _ := json.MarshalIndent(process, "", "  ")
	if err := os.WriteFile(pidFile, append(raw, '\n'), 0644); err != nil {
		shared.Failf("write pidfile: %v", err)
	}

	// Record the launch parameters: restart relaunches with the same config.
	if err := shared.WriteStartParams(pidFile, shared.StartParams{
		DevDB: databasePath, DevCache: cachePath, DevEvents: eventsPath, Host: host,
		Port: vitePort, AdminPort: adminPort, LogLevel: logLevel,
		EventsRetentionDays: retentionDays,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "[WARN] write params file: %v\n", err)
	}

	// Give the OS a moment to report a very early exit (e.g. missing binary)
	// without doing a health poll. This is not a readiness check.
	time.Sleep(200 * time.Millisecond)

	if backendOnly && frontendPID > 0 {
		fmt.Printf("[OK] Restarted backend PID %d, frontend kept as PID %d\n", backendPID, frontendPID)
	} else {
		fmt.Printf("[OK] Started backend PID %d, frontend PID %d\n", backendPID, frontendPID)
	}
	fmt.Printf("Dashboard (dev): http://%s:%s\n", host, vitePort)
	fmt.Printf("API: http://%s:%s/v1 (backend direct: http://%s:%s/v1)\n", host, vitePort, host, shared.DevBackendPort)
	if adminPort != "" {
		fmt.Printf("Admin (loopback, no auth): http://127.0.0.1:%s\n", adminPort)
	}
	fmt.Printf("Events: %s (query: make log)\n", eventsPath)
	fmt.Printf("Frontend log: %s\n", frontendLog)
	fmt.Printf("Pidfile: %s\n", pidFile)
}

func mustAtoi(portString string) int {
	number := 0
	for _, digit := range portString {
		if digit < '0' || digit > '9' {
			shared.Failf("invalid port %q", portString)
		}
		number = number*10 + int(digit-'0')
	}
	return number
}

// snapshotPath records the executable identity for a spawned PID. The live
// process path wins. The fallback covers a query that lands before exec
// completes or without query rights.
func snapshotPath(pid int, fallback string) string {
	if actual := shared.ProcessPath(pid); actual != "unknown" && actual != "" {
		return actual
	}
	return fallback
}

// resolveBun locates the frontend binary for the identity fallback.
func resolveBun() string {
	if resolved, err := exec.LookPath("bun"); err == nil {
		if abs, err := filepath.Abs(resolved); err == nil {
			return abs
		}
		return resolved
	}
	return ""
}
