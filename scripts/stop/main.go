package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

func main() {
	pidFile := flag.String("pid-file", shared.DefaultPidFile(), "pidfile path")
	flag.Parse()

	if _, err := os.Stat(*pidFile); os.IsNotExist(err) {
		fmt.Printf("[>] Not running (no pid file %s)\n", *pidFile)
		return
	}

	p, err := shared.ReadPidFile(*pidFile)
	if err != nil {
		shared.Failf("parse pidfile %s: %v", *pidFile, err)
	}

	type entry struct {
		name string
		pid  int
	}
	entries := []entry{{"backend", p.Backend}, {"frontend", p.Frontend}}
	aliveAny := false
	for _, e := range entries {
		if e.pid > 0 && shared.Alive(e.pid) {
			aliveAny = true
		}
	}
	if !aliveAny {
		fmt.Println("[>] Not running (pids not alive)")
		_ = os.Remove(*pidFile)
		fmt.Println("[OK] Stopped")
		return
	}

	// Graceful terminate only.
	for _, e := range entries {
		if e.pid <= 0 || !shared.Alive(e.pid) {
			continue
		}
		fmt.Printf("[>] Stopping %s PID %d...\n", e.name, e.pid)
		if err := shared.Terminate(e.pid); err != nil {
			shared.Failf("terminate %s PID %d: %v", e.name, e.pid, err)
		}
	}

	// Wait up to 10s.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		still := false
		for _, e := range entries {
			if e.pid > 0 && shared.Alive(e.pid) {
				still = true
				break
			}
		}
		if !still {
			_ = os.Remove(*pidFile)
			fmt.Println("[OK] Stopped")
			return
		}
		time.Sleep(200 * time.Millisecond)
	}

	var survivors []string
	for _, e := range entries {
		if e.pid > 0 && shared.Alive(e.pid) {
			survivors = append(survivors, fmt.Sprintf("%s PID %d", e.name, e.pid))
		}
	}
	fmt.Fprintf(os.Stderr, "[FAIL] Still running after grace period: %v (pidfile kept at %s)\n", survivors, *pidFile)
	os.Exit(1)
}
