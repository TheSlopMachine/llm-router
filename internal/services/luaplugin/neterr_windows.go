//go:build windows

package luaplugin

import (
	"errors"
	"syscall"
)

// Winsock and Win32 codes that the portable syscall constants do not match on
// Windows.
const (
	errBrokenPipe     syscall.Errno = 109
	errNetnameDeleted syscall.Errno = 64
	wsaConnAborted    syscall.Errno = 10053
	wsaConnReset      syscall.Errno = 10054
	wsaConnRefused    syscall.Errno = 10061
)

func isConnRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, wsaConnRefused)
}

func isConnReset(err error) bool {
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, wsaConnReset) || errors.Is(err, errNetnameDeleted)
}

// isBrokenPipe reports a write to a connection the peer already closed.
func isBrokenPipe(err error) bool {
	return errors.Is(err, syscall.EPIPE) || errors.Is(err, wsaConnAborted) || errors.Is(err, errBrokenPipe)
}
