//go:build !windows

package shared

import (
	"fmt"
	"os"
	"syscall"
)

// Alive reports whether pid exists. Signal 0 performs error checking
// without delivering a signal.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func terminate(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

// registerSession is a no-op on unix. detachedAttr already sets Setpgid,
// making the spawned process the leader of its own new process group;
// anything it forks inherits that pgid automatically, so forceKillAll's
// -pid signal already reaches the whole tree with no extra bookkeeping.
func registerSession(pidFile string, pids []int) error { return nil }

// forceKillAll sends SIGKILL to each pid's process group.
func forceKillAll(pidFile string, pids []int) error {
	var firstErr error
	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// processPath resolves pid's executable path via /proc, best-effort. Works
// on Linux; returns "unknown" on platforms without /proc (e.g. macOS) or on
// any other failure. Diagnostic-only, never load-bearing.
func processPath(pid int) string {
	link, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return "unknown"
	}
	return link
}
