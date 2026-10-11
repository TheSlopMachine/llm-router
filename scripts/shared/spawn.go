package shared

import (
	"os"
	"os/exec"
)

// SpawnDetached starts name(arguments...) in workdir with stdout/stderr truncated to
// logPath and returns the child PID. The child is detached so it survives
// the parent exiting. No shell, no nohup.
func SpawnDetached(workdir, logPath, name string, arguments ...string) (int, error) {
	return SpawnDetachedEnv(workdir, logPath, nil, name, arguments...)
}

// SpawnDetachedEnv is SpawnDetached with extra environment variables
// (appended to the parent environment).
func SpawnDetachedEnv(workdir, logPath string, extraEnv []string, name string, arguments ...string) (int, error) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()

	cmd := exec.Command(name, arguments...)
	cmd.Dir = workdir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.Env = EnvWith(extraEnv)
	cmd.SysProcAttr = newConsoleAttr()

	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}
