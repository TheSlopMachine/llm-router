package main

import (
	"fmt"
	"os"
	"time"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

const (
	gracePeriod        = 3 * time.Second
	forceConfirmPeriod = 2 * time.Second
	pollInterval       = 200 * time.Millisecond
)

func main() {
	pidFile := shared.Getenv("PID_FILE", shared.DefaultPidFile())

	if _, err := os.Stat(pidFile); os.IsNotExist(err) {
		fmt.Printf("[>] Not running (no pid file %s)\n", pidFile)
		return
	}

	p, err := shared.ReadPidFile(pidFile)
	if err != nil {
		shared.Failf("parse pidfile %s: %v", pidFile, err)
	}

	type entry struct {
		name string
		pid  int
	}
	entries := []entry{{"backend", p.Backend}, {"frontend", p.Frontend}}

	anyAlive := func() bool {
		for _, e := range entries {
			if e.pid > 0 && shared.Alive(e.pid) {
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
		fmt.Println("[>] Not running (pids not alive)")
		_ = os.Remove(pidFile)
		fmt.Println("[OK] Stopped")
		return
	}

	// Best-effort graceful signal. Failure falls through to the
	// wait/force path below.
	for _, e := range entries {
		if e.pid <= 0 || !shared.Alive(e.pid) {
			continue
		}
		fmt.Printf("[>] Stopping %s PID %d...\n", e.name, e.pid)
		if err := shared.Terminate(e.pid); err != nil {
			fmt.Printf("[>] Graceful signal to %s PID %d failed: %v\n", e.name, e.pid, err)
			fmt.Println("[>] Force-kill follows the grace period")
		}
	}

	if waitUntilDead(time.Now().Add(gracePeriod)) {
		_ = os.Remove(pidFile)
		fmt.Println("[OK] Stopped")
		return
	}

	fmt.Println("[>] Still running after grace period, forcing shutdown...")
	pids := make([]int, 0, len(entries))
	for _, e := range entries {
		pids = append(pids, e.pid)
	}
	if err := shared.ForceKillAll(pidFile, pids...); err != nil {
		fmt.Printf("[>] force-kill error: %v\n", err)
	}

	if waitUntilDead(time.Now().Add(forceConfirmPeriod)) {
		_ = os.Remove(pidFile)
		fmt.Println("[OK] Stopped")
		return
	}

	var survivors []string
	for _, e := range entries {
		if e.pid > 0 && shared.Alive(e.pid) {
			survivors = append(survivors, fmt.Sprintf("%s PID %d", e.name, e.pid))
		}
	}
	fmt.Fprintf(os.Stderr, "[FAIL] Still running after forced shutdown: %v (pidfile kept at %s)\n", survivors, pidFile)
	os.Exit(1)
}
