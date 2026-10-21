//go:build windows

package instance

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockOffset places the locked byte far behind the record so other
// processes can still read the record of the holder: Windows byte-range
// locks block reads inside the locked range.
const lockOffset = 1 << 20

func lockFile(file *os.File) error {
	overlapped := windows.Overlapped{Offset: lockOffset}
	err := windows.LockFileEx(windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return errLocked
	}
	return err
}

func unlockFile(file *os.File) error {
	overlapped := windows.Overlapped{Offset: lockOffset}
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
}
