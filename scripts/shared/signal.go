package shared

// Terminate sends one graceful stop signal to pid. Unix: SIGTERM. Windows:
// CTRL_BREAK_EVENT to the process group. Delivery can fail before reaching
// the target (see terminate in signal_windows.go), so callers proceed to
// the force path on failure.
func Terminate(pid int) error { return terminate(pid) }

// RegisterSession groups the processes spawned for pidFile so a later,
// unrelated ForceKillAll call reaches all of them, including descendants
// on Windows. Call once from start, right after spawning. Safe to skip:
// Terminate/ForceKillAll per-PID paths still work without it, minus the
// descendant guarantee on Windows.
func RegisterSession(pidFile string, pids ...int) error {
	return registerSession(pidFile, pids)
}

// ForceKillAll terminates pids and, where RegisterSession succeeded, their
// descendants. Call only after Terminate's grace period expires. Treat
// failure here as fatal.
func ForceKillAll(pidFile string, pids ...int) error {
	return forceKillAll(pidFile, pids)
}

// ProcessPath returns the executable path of pid, best-effort. "unknown"
// when unavailable through permissions, exit, or platform. Diagnostic only.
func ProcessPath(pid int) string { return processPath(pid) }
