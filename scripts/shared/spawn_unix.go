//go:build !windows

package shared

import "syscall"

func detachedAttr() *syscall.SysProcAttr {
	// New session (implies a new group): no controlling terminal, so
	// SIGHUP on terminal close cannot reach the child. Group ID still
	// equals the PID, so group-directed signals keep working.
	return &syscall.SysProcAttr{Setsid: true}
}
