package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

const (
	gracePeriod        = 3 * time.Second
	forceConfirmPeriod = 2 * time.Second
	pollInterval       = 200 * time.Millisecond
	// shutdownRequestTimeout bounds one admin shutdown POST.
	shutdownRequestTimeout = 5 * time.Second
	// gracefulWait bounds the backend graceful exit before the kill
	// path applies. It covers the plugin hook gate plus listener, drain
	// and vacuum tails; the wait ends early when the PID dies.
	gracefulWait = 45 * time.Second
	// readyWait bounds the admin listener wait: the backend binds only
	// after its 30s startup gate, so an immediate stop outwaits it
	// instead of dropping to the kill path.
	readyWait = 30 * time.Second
)

func main() {
	pidFile := shared.GetenvPath("PID_FILE", shared.DefaultPidFile())

	if _, err := os.Stat(pidFile); os.IsNotExist(err) {
		fmt.Printf("[>] Not running (no pid file %s)\n", pidFile)
		return
	}

	process, err := shared.ReadPidFile(pidFile)
	if err != nil {
		shared.Failf("parse pidfile %s: %v", pidFile, err)
	}

	type entry struct {
		name string
		pid  int
		path string
	}
	// SCOPE selects the stop set: all (default) or backend only. The
	// frontend is never asked, only killed.
	backendOnly := false
	switch scope := strings.TrimSpace(shared.Getenv("SCOPE", "all")); scope {
	case "", "all":
	case "backend":
		backendOnly = true
	default:
		shared.Failf("invalid SCOPE %q: want all or backend", scope)
	}
	entries := []entry{
		{"backend", process.Backend, process.BackendPath},
	}
	if !backendOnly {
		entries = append(entries, entry{"frontend", process.Frontend, process.FrontendPath})
	}

	anyAlive := func() bool {
		for _, candidate := range entries {
			if shared.AliveMatches(candidate.pid, candidate.path) {
				return true
			}
		}
		return false
	}
	waitUntilDead := func(deadline time.Time) bool {
		for time.Now().Before(deadline) {
			if !anyAlive() {
				return true
			}
			time.Sleep(pollInterval)
		}
		return !anyAlive()
	}

	if !anyAlive() {
		if backendOnly {
			fmt.Println("[>] Backend not running")
		} else {
			fmt.Println("[>] Not running (pids not alive)")
		}
		finishStop(pidFile, process, backendOnly)
		return
	}

	// Graceful backend shutdown via the admin listener comes first on
	// every OS; the kill paths below stay as the bounded fallback. The
	// frontend is always killed, never asked.
	if shared.AliveMatches(process.Backend, process.BackendPath) {
		if adminPort := adminPortForStop(pidFile); adminPort != "" {
			adminURL := "http://127.0.0.1:" + adminPort
			fmt.Println("[>] Waiting for backend to accept connections...")
			if waitAdminReady(adminURL, process.Backend, process.BackendPath, time.Now().Add(readyWait)) {
				fmt.Printf("[>] Requesting backend shutdown via %s...\n", adminURL)
				if err := postShutdown(adminURL); err != nil {
					fmt.Printf("[>] Shutdown request failed: %v\n", err)
					fmt.Println("[>] Kill path follows")
				} else if waitBackendDead(process.Backend, process.BackendPath, time.Now().Add(gracefulWait)) {
					fmt.Println("[>] Backend shut down gracefully")
				} else {
					fmt.Println("[>] Backend still running after graceful wait, killing...")
				}
			} else if shared.AliveMatches(process.Backend, process.BackendPath) {
				fmt.Println("[>] Backend not accepting connections, kill path follows")
			}
			// Backend died mid-wait: fall through to the kill-phase
			// check below, which finds nothing and finishes.
		}
	}

	if runtime.GOOS != "windows" {
		// Best-effort graceful signal. Failure falls through to the
		// wait/force path below.
		for _, candidate := range entries {
			if !shared.AliveMatches(candidate.pid, candidate.path) {
				continue
			}
			fmt.Printf("[>] Stopping %s PID %d...\n", candidate.name, candidate.pid)
			if err := shared.Terminate(candidate.pid); err != nil {
				fmt.Printf("[>] Graceful signal to %s PID %d failed: %v\n", candidate.name, candidate.pid, err)
				fmt.Println("[>] Force-kill follows the grace period")
			}
		}

		if waitUntilDead(time.Now().Add(gracePeriod)) {
			finishStop(pidFile, process, backendOnly)
			return
		}

		fmt.Println("[>] Still running after grace period, forcing shutdown...")
	} else {
		for _, candidate := range entries {
			if !shared.AliveMatches(candidate.pid, candidate.path) {
				continue
			}
			fmt.Printf("[>] Stopping %s PID %d...\n", candidate.name, candidate.pid)
		}
		fmt.Println("[>] Forcing shutdown...")
	}
	targets := make([]shared.KillTarget, 0, len(entries))
	for _, candidate := range entries {
		targets = append(targets, shared.KillTarget{PID: candidate.pid, ExpectedPath: candidate.path})
	}
	if err := shared.ForceKillTargets(pidFile, targets...); err != nil {
		fmt.Printf("[>] force-kill error: %v\n", err)
	}

	if waitUntilDead(time.Now().Add(forceConfirmPeriod)) {
		finishStop(pidFile, process, backendOnly)
		return
	}

	var survivors []string
	for _, candidate := range entries {
		if shared.AliveMatches(candidate.pid, candidate.path) {
			survivors = append(survivors, fmt.Sprintf("%s PID %d", candidate.name, candidate.pid))
		}
	}
	fmt.Fprintf(os.Stderr, "[FAIL] Still running after forced shutdown: %v (pidfile kept at %s)\n", survivors, pidFile)
	os.Exit(1)
}

// adminPortForStop resolves the admin listener port from the recorded
// start params, else ADMIN_PORT env. Empty means disabled or unknown:
// the kill path applies without a graceful attempt.
func adminPortForStop(pidFile string) string {
	if params, err := shared.ReadStartParams(pidFile); err == nil {
		return strings.TrimSpace(params.AdminPort)
	}
	return strings.TrimSpace(shared.Getenv("ADMIN_PORT", ""))
}

// waitAdminReady polls the admin status until it answers 200 or the
// deadline passes. Returns false early when the backend PID dies, so a
// crash mid-startup skips to the kill-phase check without burning the
// wait.
func waitAdminReady(adminURL string, backendPID int, backendPath string, deadline time.Time) bool {
	client := &http.Client{Timeout: shutdownRequestTimeout}
	for time.Now().Before(deadline) {
		if !shared.AliveMatches(backendPID, backendPath) {
			return false
		}
		request, err := http.NewRequest(http.MethodGet, adminURL+"/api/llm-router/status", nil)
		if err == nil {
			response, err := client.Do(request)
			if err == nil {
				response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return true
				}
			}
		}
		time.Sleep(pollInterval)
	}
	return false
}

// postShutdown asks the admin listener for graceful shutdown, expecting
// a 2xx answer.
func postShutdown(adminURL string) error {
	client := &http.Client{Timeout: shutdownRequestTimeout}
	request, err := http.NewRequest(http.MethodPost, adminURL+"/api/llm-router/shutdown", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("http %d", response.StatusCode)
	}
	return nil
}

// waitBackendDead polls the backend PID until deadline.
func waitBackendDead(backendPID int, backendPath string, deadline time.Time) bool {
	for time.Now().Before(deadline) {
		if !shared.AliveMatches(backendPID, backendPath) {
			return true
		}
		time.Sleep(pollInterval)
	}
	return !shared.AliveMatches(backendPID, backendPath)
}

// finishStop closes a successful stop: the pidfile goes away when
// nothing tracked is alive. In backend-only scope with a live frontend
// the pidfile is rewritten with backend fields zeroed instead, so the
// frontend stays manageable.
func finishStop(pidFile string, process *shared.Proc, backendOnly bool) {
	if backendOnly && process.Frontend > 0 && shared.AliveMatches(process.Frontend, process.FrontendPath) {
		updated := *process
		updated.Backend = 0
		updated.BackendPath = ""
		raw, err := json.MarshalIndent(updated, "", "  ")
		if err == nil {
			err = os.WriteFile(pidFile, append(raw, '\n'), 0644)
		}
		if err != nil {
			shared.Failf("write pidfile %s: %v", pidFile, err)
		}
		fmt.Println("[OK] Stopped (backend only, frontend kept)")
		return
	}
	_ = os.Remove(pidFile)
	fmt.Println("[OK] Stopped")
}
