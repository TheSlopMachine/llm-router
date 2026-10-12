package shared

import "errors"

// ErrGracefulUnsupported reports a graceful stop request on a platform
// without a graceful path. Windows stops force-kill directly.
var ErrGracefulUnsupported = errors.New("graceful stop unsupported on windows")

// Terminate sends one graceful stop signal to pid's process group (Unix:
// SIGTERM to -pid). Windows always returns ErrGracefulUnsupported;
// callers stop through the force path directly.
func Terminate(pid int) error { return terminate(pid) }

// RegisterSession groups the processes spawned for pidFile so a later,
// unrelated ForceKillAll call reaches all of them, including descendants
// on Windows. Call once from start, right after spawning. Safe to skip:
// ForceKillAll per-PID paths still work without it, minus the
// descendant guarantee on Windows.
func RegisterSession(pidFile string, pids ...int) error {
	return registerSession(pidFile, pids)
}

// ForceKillAll terminates pids and, where RegisterSession succeeded, their
// descendants. Unix callers wait for the Terminate grace period first;
// Windows callers invoke directly. Treat failure here as fatal.
func ForceKillAll(pidFile string, pids ...int) error {
	return forceKillAll(pidFile, pids)
}

// ProcessPath returns the executable path of pid, best-effort. "unknown"
// when unavailable through permissions, exit, or platform. Status reports
// it. AliveMatches uses it as process identity with a conservative
// fallback to alive on "unknown".
func ProcessPath(pid int) string { return processPath(pid) }
