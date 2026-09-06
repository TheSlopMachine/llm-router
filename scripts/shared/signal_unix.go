//go:build !windows

package shared

import "syscall"

// Alive reports whether pid exists. Signal 0 performs error checking
// without delivering a signal.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func terminate(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }
