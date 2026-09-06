package shared

// Terminate sends one graceful stop signal to pid and returns nil if the
// signal was delivered. Unix: SIGTERM. Windows: CTRL_BREAK_EVENT to the
// process group (see terminate in signal_windows.go for why this, and not
// TerminateProcess, is the graceful option there).
// It never escalates: if the process survives, that is the caller's problem.
func Terminate(pid int) error { return terminate(pid) }
