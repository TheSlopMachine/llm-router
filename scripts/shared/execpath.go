package shared

import (
	"path/filepath"
	"runtime"
	"strings"
)

// NormalizeExecPath returns a comparable form of an executable path.
// Windows comparison is case-insensitive with unified separators.
// Empty input yields empty output.
func NormalizeExecPath(input string) string {
	normalizedPath := strings.TrimSpace(input)
	if normalizedPath == "" {
		return ""
	}
	if strings.HasPrefix(normalizedPath, `\\?\`) {
		normalizedPath = strings.TrimPrefix(normalizedPath, `\\?\`)
		if strings.HasPrefix(normalizedPath, `UNC\`) {
			normalizedPath = `\\` + strings.TrimPrefix(normalizedPath, `UNC\`)
		}
	}
	if runtime.GOOS == "windows" {
		normalizedPath = strings.ReplaceAll(normalizedPath, "/", `\`)
		normalizedPath = filepath.Clean(normalizedPath)
		return strings.ToLower(normalizedPath)
	}
	normalizedPath = strings.ReplaceAll(normalizedPath, `\`, "/")
	return filepath.Clean(normalizedPath)
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
	for _, target := range targets {
		if target.PID <= 0 {
			continue
		}
		if !Alive(target.PID) {
			continue
		}
		if strings.TrimSpace(target.ExpectedPath) != "" {
			actual := processPath(target.PID)
			if actual != "unknown" && !SameExecutable(actual, target.ExpectedPath) {
				continue
			}
		}
		matched = append(matched, target.PID)
	}
	if len(matched) == 0 {
		return nil
	}
	return forceKillAll(pidFile, matched)
}
