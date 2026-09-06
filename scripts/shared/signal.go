package shared

// Terminate sends one graceful stop signal to pid and returns nil if the
// signal was delivered. Unix: SIGTERM. Windows: taskkill /PID (no /F, no /T).
// It never escalates: if the process survives, that is the caller's problem.
func Terminate(pid int) error { return terminate(pid) }
