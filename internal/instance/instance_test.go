package instance

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireBlocksSecondHolder(t *testing.T) {
	path := LockPath(filepath.Join(t.TempDir(), "router.sqlite3"))
	first, err := Acquire(path, Info{Mode: ModeDaemon, Address: "localhost:8080", Version: "1.2"})
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	_, err = Acquire(path, Info{Mode: ModeForeground})
	var held *HeldError
	if !errors.As(err, &held) || !errors.Is(err, ErrHeld) {
		t.Fatalf("second Acquire error = %v, want *HeldError", err)
	}
	if held.Info.PID != os.Getpid() || held.Info.Mode != ModeDaemon || held.Info.Address != "localhost:8080" || held.Info.Version != "1.2" {
		t.Errorf("holder = %+v", held.Info)
	}
	if held.Info.Started.IsZero() {
		t.Error("holder start time is zero")
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	second, err := Acquire(path, Info{Mode: ModeForeground})
	if err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
	_ = second.Release()
}

func TestProbe(t *testing.T) {
	path := LockPath(filepath.Join(t.TempDir(), "router.sqlite3"))
	if _, held, err := Probe(path); err != nil || held {
		t.Fatalf("Probe on missing file = held %v, err %v", held, err)
	}
	lock, err := Acquire(path, Info{Mode: ModeForeground})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	info, held, err := Probe(path)
	if err != nil || !held || info.PID != os.Getpid() || info.Mode != ModeForeground {
		t.Fatalf("Probe while held = %+v, %v, %v", info, held, err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, held, err := Probe(path); err != nil || held {
		t.Fatalf("Probe after Release = held %v, err %v", held, err)
	}
}

func TestProbeDoesNotBlockAcquire(t *testing.T) {
	path := LockPath(filepath.Join(t.TempDir(), "router.sqlite3"))
	if _, err := os.Create(path); err != nil {
		t.Fatal(err)
	}
	if _, held, err := Probe(path); err != nil || held {
		t.Fatalf("Probe on unlocked file = held %v, err %v", held, err)
	}
	lock, err := Acquire(path, Info{})
	if err != nil {
		t.Fatalf("Acquire after Probe: %v", err)
	}
	_ = lock.Release()
}

func TestReleaseTwiceIsSafe(t *testing.T) {
	path := LockPath(filepath.Join(t.TempDir(), "router.sqlite3"))
	lock, err := Acquire(path, Info{})
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err != nil {
		t.Errorf("second Release = %v, want nil", err)
	}
	var missing *Lock
	if err := missing.Release(); err != nil {
		t.Errorf("nil Release = %v, want nil", err)
	}
}
