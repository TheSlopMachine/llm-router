package shared

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ScriptsDir returns the scripts/ module directory.
func ScriptsDir() (string, error) {
	root, err := RootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "scripts"), nil
}

// RunScript runs another script in this module as
// `go run ./<name> <args...>` with GOWORK=off, inheriting stdio.
func RunScript(name string, args ...string) error {
	dir, err := ScriptsDir()
	if err != nil {
		return err
	}
	cmdArgs := append([]string{"run", "./" + name}, args...)
	cmd := exec.Command("go", cmdArgs...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("script %s: %w", name, err)
	}
	return nil
}
