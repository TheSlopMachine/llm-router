package shared

import (
	"path/filepath"
	"runtime"
	"strings"
)

// NormalizeExecPath returns a comparable form of an executable path.
// Windows comparison is case-insensitive with unified separators.
// Empty input yields empty output.
func NormalizeExecPath(p string) string {
	q := strings.TrimSpace(p)
	if q == "" {
		return ""
	}
	if strings.HasPrefix(q, `\\?\`) {
		q = strings.TrimPrefix(q, `\\?\`)
		if strings.HasPrefix(q, `UNC\`) {
			q = `\\` + strings.TrimPrefix(q, `UNC\`)
		}
	}
	if runtime.GOOS == "windows" {
		q = strings.ReplaceAll(q, "/", `\`)
		q = filepath.Clean(q)
		return strings.ToLower(q)
	}
	q = strings.ReplaceAll(q, `\`, "/")
	return filepath.Clean(q)
}

// SameExecutable reports whether actual and expected name the same binary.
// Empty expected preserves legacy pidfiles without recorded paths.
func SameExecutable(actual, expected string) bool {
	if strings.TrimSpace(expected) == "" {
		return true
	}
	if strings.TrimSpace(actual) == "" {
		return false
	}
	return NormalizeExecPath(actual) == NormalizeExecPath(expected)
}

// KillTarget pairs a PID with the executable path recorded at start.
type KillTarget struct {
	PID          int
	ExpectedPath string
}

// AliveMatches reports whether pid is alive and still the recorded binary.
// A reused PID now owned by a different executable counts as not alive.
// An unresolvable actual path ("unknown") stays conservative and counts as
// alive to avoid removing a pidfile blind.
func AliveMatches(pid int, expectedPath string) bool {
	if pid <= 0 {
		return false
	}
	if !Alive(pid) {
		return false
	}
	if strings.TrimSpace(expectedPath) == "" {
		return true
	}
	actual := processPath(pid)
	if actual == "unknown" {
		return true
	}
	return SameExecutable(actual, expectedPath)
}

// ForceKillTargets terminates only targets still owned by their recorded
// binaries, then delegates to the platform force path. Reused PIDs never
// reach the kill call. An empty match set returns nil.
func ForceKillTargets(pidFile string, targets ...KillTarget) error {
	matched := make([]int, 0, len(targets))
	for _, t := range targets {
		if t.PID <= 0 {
			continue
		}
		if !Alive(t.PID) {
			continue
		}
		if strings.TrimSpace(t.ExpectedPath) != "" {
			actual := processPath(t.PID)
			if actual != "unknown" && !SameExecutable(actual, t.ExpectedPath) {
				continue
			}
		}
		matched = append(matched, t.PID)
	}
	if len(matched) == 0 {
		return nil
	}
	return forceKillAll(pidFile, matched)
}
