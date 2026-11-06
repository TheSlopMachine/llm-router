package cmd

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/instance"
)

func TestWaitForReleaseWaitsPastNotice(t *testing.T) {
	lockPath := instance.LockPath(filepath.Join(t.TempDir(), "router.sqlite3"))
	lock, err := instance.Acquire(lockPath, instance.Info{Mode: instance.ModeDaemon})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	go func() {
		time.Sleep(600 * time.Millisecond)
		_ = lock.Release()
	}()
	notices := 0
	if err := waitForRelease(lockPath, 5*time.Second, 100*time.Millisecond, func() { notices++ }); err != nil {
		t.Fatalf("waitForRelease: %v", err)
	}
	if notices != 1 {
		t.Errorf("notices = %d, want 1", notices)
	}
}

func TestWaitForReleaseReportsHeldLock(t *testing.T) {
	lockPath := instance.LockPath(filepath.Join(t.TempDir(), "router.sqlite3"))
	lock, err := instance.Acquire(lockPath, instance.Info{Mode: instance.ModeDaemon})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lock.Release()
	err = waitForRelease(lockPath, 300*time.Millisecond, time.Hour, func() {})
	if !errors.Is(err, errLockStillHeld) {
		t.Errorf("waitForRelease error = %v, want errLockStillHeld", err)
	}
}

func TestWaitForReleaseWithoutLockFile(t *testing.T) {
	lockPath := instance.LockPath(filepath.Join(t.TempDir(), "router.sqlite3"))
	if err := waitForRelease(lockPath, time.Second, time.Hour, func() {}); err != nil {
		t.Errorf("waitForRelease: %v", err)
	}
}
