//go:build !windows

package cmd

import "syscall"

const daemonSupported = true

// detachedAttributes puts the child in its own session so it survives the
// terminal that started it.
func detachedAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

func terminateProcess(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}
