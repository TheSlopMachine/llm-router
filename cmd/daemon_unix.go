//go:build !windows

package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// detachedAttributes puts the child in its own session so it survives the
// terminal that started it.
func detachedAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// requestStop sends SIGTERM to process pid.
func requestStop(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}

// shutdownContext ends on SIGINT or SIGTERM.
func shutdownContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	return ctx, stop, nil
}
