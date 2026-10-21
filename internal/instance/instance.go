// Package instance guards one llm-router process per database. The lock
// file next to the database carries a kernel file lock, so a crashed
// process never leaves a stale lock behind, and a JSON record of the
// holder for status, stop and error messages.
package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// Mode names how the lock holder was started.
type Mode string

const (
	ModeForeground  Mode = "foreground"
	ModeDaemon      Mode = "daemon"
	ModeMaintenance Mode = "maintenance"
)

// ErrHeld matches the error returned when another process holds the lock.
var ErrHeld = errors.New("another llm-router instance holds the database")

// errLocked is the platform-neutral result of a failed non-blocking lock.
var errLocked = errors.New("file is locked")

const (
	acquireAttempts = 5
	acquireDelay    = 50 * time.Millisecond
)

// Info describes the lock holder.
type Info struct {
	PID        int       `json:"pid"`
	Mode       Mode      `json:"mode"`
	Started    time.Time `json:"started"`
	Executable string    `json:"executable"`
	Address    string    `json:"address"`
	Version    string    `json:"version"`
}

// HeldError reports the holder of a lock that Acquire could not take.
type HeldError struct {
	Info Info
}

func (err *HeldError) Error() string {
	if err.Info.PID == 0 {
		return ErrHeld.Error()
	}
	return fmt.Sprintf("%s (pid %d, %s)", ErrHeld.Error(), err.Info.PID, err.Info.Mode)
}

func (err *HeldError) Unwrap() error { return ErrHeld }

// Lock is a held instance lock.
type Lock struct {
	file *os.File
}

// LockPath returns the lock file for a database path.
func LockPath(databasePath string) string {
	return databasePath + ".lock"
}

// Acquire takes the exclusive lock and records info. A zero PID and Started
// are filled from the current process. A held lock returns *HeldError.
func Acquire(path string, info Info) (*Lock, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}
	for attempt := 1; ; attempt++ {
		err = lockFile(file)
		if err == nil {
			break
		}
		if !errors.Is(err, errLocked) {
			_ = file.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		holder, _ := readInfo(path)
		// An empty record means the holder is between locking and writing
		// its record, or a probe holds the lock for a moment.
		if holder.PID != 0 || attempt == acquireAttempts {
			_ = file.Close()
			return nil, &HeldError{Info: holder}
		}
		time.Sleep(acquireDelay)
	}
	if info.PID == 0 {
		info.PID = os.Getpid()
	}
	if info.Started.IsZero() {
		info.Started = time.Now().UTC()
	}
	record, err := json.Marshal(info)
	if err != nil {
		releaseFile(file)
		return nil, fmt.Errorf("encode lock record: %w", err)
	}
	if err := file.Truncate(0); err != nil {
		releaseFile(file)
		return nil, fmt.Errorf("truncate %s: %w", path, err)
	}
	if _, err := file.WriteAt(record, 0); err != nil {
		releaseFile(file)
		return nil, fmt.Errorf("write %s: %w", path, err)
	}
	_ = file.Sync()
	return &Lock{file: file}, nil
}

// Release clears the record and drops the lock. The file stays: removing it
// would let two processes lock different files of the same name.
func (lock *Lock) Release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	file := lock.file
	lock.file = nil
	_ = file.Truncate(0)
	return releaseFile(file)
}

// Probe reports whether a process holds the lock at path and who it is. A
// missing file means nobody holds it.
func Probe(path string) (Info, bool, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Info{}, false, nil
		}
		return Info{}, false, fmt.Errorf("open lock file %s: %w", path, err)
	}
	err = lockFile(file)
	if err == nil {
		_ = unlockFile(file)
		_ = file.Close()
		return Info{}, false, nil
	}
	_ = file.Close()
	if !errors.Is(err, errLocked) {
		return Info{}, false, fmt.Errorf("probe %s: %w", path, err)
	}
	info, _ := readInfo(path)
	return info, true, nil
}

func readInfo(path string) (Info, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Info{}, err
	}
	var info Info
	if err := json.Unmarshal(raw, &info); err != nil {
		return Info{}, err
	}
	return info, nil
}

func releaseFile(file *os.File) error {
	unlockErr := unlockFile(file)
	closeErr := file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
