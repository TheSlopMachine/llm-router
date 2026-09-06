package shared

import (
	"os"
	"os/exec"
)

// SpawnDetached starts name(args...) in dir with stdout/stderr truncated to
// logPath and returns the child PID. The child is detached so it survives
// the parent exiting. No shell, no nohup.
func SpawnDetached(dir, logPath, name string, args ...string) (int, error) {
	return SpawnDetachedEnv(dir, logPath, nil, name, args...)
}

// SpawnDetachedEnv is SpawnDetached with extra environment variables
// (appended to the parent environment).
func SpawnDetachedEnv(dir, logPath string, extraEnv []string, name string, args ...string) (int, error) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.Env = EnvWith(extraEnv)
	cmd.SysProcAttr = detachedAttr()

	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}
