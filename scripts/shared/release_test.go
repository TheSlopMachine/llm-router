package shared

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseBinaryName(t *testing.T) {
	if got := BinaryName("windows"); got != "llm-router.exe" {
		t.Errorf("windows binary = %q, want llm-router.exe", got)
	}
	for _, targetOS := range []string{"linux", "darwin", "freebsd"} {
		if got := BinaryName(targetOS); got != "llm-router" {
			t.Errorf("%s binary = %q, want llm-router", targetOS, got)
		}
	}
}

func TestReleaseLdflagsContent(t *testing.T) {
	flags := ReleaseLdflags("v1.2.3", "abc123", "2026-10-13T00:00:00Z")
	for _, want := range []string{"main.Version=v1.2.3", "main.GitCommit=abc123", "main.BuildTime=2026-10-13T00:00:00Z"} {
		if !strings.Contains(flags, want) {
			t.Errorf("ldflags = %q, want %q", flags, want)
		}
	}
}

func TestReleaseResolveVersionExplicit(t *testing.T) {
	t.Setenv("VERSION", "v9.9.9")
	if got := ResolveVersion(); got != "v9.9.9" {
		t.Errorf("version = %q, want v9.9.9", got)
	}
}

func TestReleaseInstallBinDirectory(t *testing.T) {
	home := t.TempDir()
	setHomeDirectory(t, home)
	directory, err := InstallBinDirectory()
	if err != nil {
		t.Fatalf("InstallBinDirectory: %v", err)
	}
	want := filepath.Join(home, ".local", "bin")
	if directory != want {
		t.Errorf("bin directory = %q, want %q", directory, want)
	}
}

func TestReleaseBinDirectoryInPath(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "bin")
	other := filepath.Join(t.TempDir(), "other")
	separator := string(os.PathListSeparator)
	t.Setenv("PATH", strings.Join([]string{other, directory}, separator))
	if !BinDirectoryInPath(directory) {
		t.Errorf("BinDirectoryInPath(%q) = false, want true", directory)
	}
	t.Setenv("PATH", other)
	if BinDirectoryInPath(directory) {
		t.Errorf("BinDirectoryInPath(%q) = true, want false", directory)
	}
	if BinDirectoryInPath("") {
		t.Error("BinDirectoryInPath(empty) = true, want false")
	}
}

func TestReleasePathSetupHint(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "bin")
	hint := PathSetupHint(directory)
	if !strings.Contains(hint, directory) {
		t.Errorf("hint misses directory:\n%s", hint)
	}
}
