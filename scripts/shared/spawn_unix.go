//go:build !windows

package shared

import "syscall"

func detachedAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }
