//go:build !windows

package luaplugin

import (
	"errors"
	"syscall"
)

func isConnRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }

func isConnReset(err error) bool { return errors.Is(err, syscall.ECONNRESET) }

// isBrokenPipe reports a write to a connection the peer already closed.
func isBrokenPipe(err error) bool { return errors.Is(err, syscall.EPIPE) }
