package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/instance"
)

// childLockEnvironment makes the test binary act as a starting daemon that
// holds the lock at the given path, never marks it ready and exits on a
// stop request.
const childLockEnvironment = "LLM_ROUTER_TEST_CHILD_LOCK"

func TestMain(m *testing.M) {
	if lockPath := os.Getenv(childLockEnvironment); lockPath != "" {
		os.Exit(runStartingChild(lockPath))
	}
	os.Exit(m.Run())
}

func runStartingChild(lockPath string) int {
	ctx, stop, err := shutdownContext(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer stop()
	lock, err := instance.Acquire(lockPath, instance.Info{Mode: instance.ModeDaemon})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer lock.Release()
	<-ctx.Done()
	return 0
}

func TestAwaitStartStopsChildOnInterrupt(t *testing.T) {
	directory := t.TempDir()
	lockPath := instance.LockPath(filepath.Join(directory, "router.sqlite3"))
	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), childLockEnvironment+"="+lockPath)
	if err := child.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	interrupts := make(chan os.Signal, 1)
	result := make(chan error, 1)
	go func() {
		_, err := awaitStart(child.Process, exited, interrupts, lockPath, filepath.Join(directory, "daemon.log"))
		result <- err
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		info, held, err := instance.Probe(lockPath)
		if err != nil {
			t.Fatalf("Probe: %v", err)
		}
		if held && info.PID == child.Process.Pid {
			break
		}
		if time.Now().After(deadline) {
			_ = child.Process.Kill()
			t.Fatal("child did not take the lock")
		}
		time.Sleep(daemonPollInterval)
	}
	interrupts <- os.Interrupt

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("awaitStart succeeded after an interrupt")
		}
	case <-time.After(30 * time.Second):
		_ = child.Process.Kill()
		t.Fatal("awaitStart did not return after an interrupt")
	}
	if !child.ProcessState.Success() {
		t.Errorf("child state = %v, want a clean exit after the stop request", child.ProcessState)
	}
	if _, held, err := instance.Probe(lockPath); err != nil || held {
		t.Errorf("lock held = %v, error = %v after abort", held, err)
	}
}

func TestRequestStopEndsShutdownContext(t *testing.T) {
	ctx, release, err := shutdownContext(context.Background())
	if err != nil {
		t.Fatalf("shutdownContext: %v", err)
	}
	defer release()
	if err := requestStop(os.Getpid()); err != nil {
		t.Fatalf("requestStop: %v", err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown context did not end after requestStop")
	}
}

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
