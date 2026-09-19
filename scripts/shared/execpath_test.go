package shared

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeExecPathEmpty(t *testing.T) {
	if got := NormalizeExecPath(""); got != "" {
		t.Fatalf("empty input yields empty output, got %q", got)
	}
}

func TestSameExecutableLegacy(t *testing.T) {
	if !SameExecutable("C:\\anything.exe", "") {
		t.Fatal("empty expected path preserves legacy match")
	}
	if SameExecutable("", "C:\\anything.exe") {
		t.Fatal("empty actual path never matches")
	}
}

func TestSameExecutableSeparators(t *testing.T) {
	a := `C:\Temp\llm-router-dev-backend.exe`
	b := "C:/Temp/llm-router-dev-backend.exe"
	if !SameExecutable(a, b) {
		t.Fatalf("%q matches %q after normalization", a, b)
	}
	if SameExecutable(a, `C:\Temp\other.exe`) {
		t.Fatal("different binaries never match")
	}
}

func TestAliveMatchesInvalidPID(t *testing.T) {
	if AliveMatches(0, "") {
		t.Fatal("PID 0 is never alive")
	}
	if AliveMatches(-1, "") {
		t.Fatal("negative PID is never alive")
	}
	if AliveMatches(0, `C:\Temp\llm-router-dev-backend.exe`) {
		t.Fatal("PID 0 with expected path is never alive")
	}
}

func TestAliveMatchesCurrentProcess(t *testing.T) {
	pid := os.Getpid()
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("executable path unavailable: %v", err)
	}
	if !AliveMatches(pid, exe) {
		t.Fatalf("current process %d matches %q", pid, exe)
	}
	if AliveMatches(pid, filepath.Join(filepath.Dir(exe), "definitely-not-this-binary.exe")) {
		t.Fatal("current process never matches a different path")
	}
}

func TestReadPidFileBackwardCompatible(t *testing.T) {
	dir := t.TempDir()

	legacy := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(legacy, []byte("1234\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := ReadPidFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if p.Backend != 1234 {
		t.Fatalf("legacy single PID parses as backend, got %+v", p)
	}

	oldShape := filepath.Join(dir, "old.json")
	if err := os.WriteFile(oldShape, []byte(`{"backend": 11, "frontend": 22, "vitePort": 8080}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p, err = ReadPidFile(oldShape)
	if err != nil {
		t.Fatal(err)
	}
	if p.Backend != 11 || p.Frontend != 22 || p.BackendPath != "" {
		t.Fatalf("old shape parses with empty paths, got %+v", p)
	}

	newShape := filepath.Join(dir, "new.json")
	if err := os.WriteFile(newShape, []byte(`{"backend": 11, "frontend": 22, "vitePort": 8080, "backendPath": "C:\\Temp\\b.exe", "frontendPath": "C:\\bun.exe"}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p, err = ReadPidFile(newShape)
	if err != nil {
		t.Fatal(err)
	}
	if p.BackendPath == "" || p.FrontendPath == "" {
		t.Fatalf("new shape keeps recorded paths, got %+v", p)
	}
}

func TestForceKillTargetsSkipsMismatch(t *testing.T) {
	pid := os.Getpid()
	if err := ForceKillTargets("nonexistent-pidfile", KillTarget{
		PID:          pid,
		ExpectedPath: filepath.Join(os.TempDir(), "definitely-not-this-binary.exe"),
	}); err != nil {
		t.Fatalf("mismatched target never reaches kill, got %v", err)
	}
	if err := ForceKillTargets("nonexistent-pidfile"); err != nil {
		t.Fatalf("empty target set returns nil, got %v", err)
	}
}
