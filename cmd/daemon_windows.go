//go:build windows

package cmd

import (
	"errors"
	"syscall"
)

const daemonSupported = false

func detachedAttributes() *syscall.SysProcAttr { return nil }

func terminateProcess(int) error {
	return errors.New("not supported")
}
