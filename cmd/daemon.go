package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TheSlopMachine/llm-router/internal/cli"
	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/instance"
)

const (
	daemonStartTimeout = 20 * time.Second
	daemonStopTimeout  = 30 * time.Second
	daemonPollInterval = 100 * time.Millisecond
	daemonLogLimit     = 1 << 20
	daemonLogTailLines = 15
	notRunningExitCode = 3
)

func newDaemonCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "daemon",
		Short: "Run the server in the background (Linux, macOS, FreeBSD)",
		Long: `Run the server in the background.

A lock file next to the database records the running process, so the
commands also find a server started with "llm-router serve". For systemd
and containers use "llm-router serve" instead: a supervisor wants the
process in the foreground.`,
		GroupID: groupRun,
		Args:    cobra.NoArgs,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return requireDaemonSupport()
		},
		RunE: func(command *cobra.Command, _ []string) error {
			if err := requireDaemonSupport(); err != nil {
				return err
			}
			return command.Help()
		},
	}
	command.AddCommand(
		&cobra.Command{
			Use:   "start",
			Short: "Start the server in the background",
			Args:  cobra.NoArgs,
			RunE:  runDaemonStart,
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Stop the running server and wait for a clean exit",
			Args:  cobra.NoArgs,
			RunE:  runDaemonStop,
		},
		&cobra.Command{
			Use:   "restart",
			Short: "Stop the server if it runs, then start it",
			Args:  cobra.NoArgs,
			RunE:  runDaemonRestart,
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show whether the server runs",
			Args:  cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				return runStatus(false)
			},
		},
	)
	return command
}

func requireDaemonSupport() error {
	if !daemonSupported {
		return fmt.Errorf("daemon is not supported on %s", runtime.GOOS)
	}
	return nil
}

func runDaemonStart(*cobra.Command, []string) error {
	applicationConfig, _, err := config.Load()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	info, err := startDaemon(applicationConfig, executable)
	if err != nil {
		return err
	}
	fmt.Printf("started (pid %d), listening on %s\n", info.PID, applicationConfig.Addr)
	return nil
}

func runDaemonStop(*cobra.Command, []string) error {
	applicationConfig, _, err := config.Load()
	if err != nil {
		return err
	}
	stopped, info, err := stopInstance(lockPathFor(applicationConfig), daemonStopTimeout)
	if err != nil {
		return err
	}
	if !stopped {
		fmt.Println("not running")
		return nil
	}
	fmt.Printf("stopped (pid %d)\n", info.PID)
	return nil
}

func runDaemonRestart(*cobra.Command, []string) error {
	applicationConfig, _, err := config.Load()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	if stopped, info, err := stopInstance(lockPathFor(applicationConfig), daemonStopTimeout); err != nil {
		return err
	} else if stopped {
		fmt.Printf("stopped (pid %d)\n", info.PID)
	}
	info, err := startDaemon(applicationConfig, executable)
	if err != nil {
		return err
	}
	fmt.Printf("started (pid %d), listening on %s\n", info.PID, applicationConfig.Addr)
	return nil
}

// startDaemon launches "executable serve" detached and waits until the
// child holds the instance lock and answers HTTP. A child that exits during
// the wait fails the start with the tail of its output.
func startDaemon(applicationConfig *config.Config, executable string) (instance.Info, error) {
	lockPath := lockPathFor(applicationConfig)
	if info, held, err := instance.Probe(lockPath); err != nil {
		return instance.Info{}, err
	} else if held {
		return instance.Info{}, &instance.HeldError{Info: info}
	}
	logPath, err := daemonLogPath(applicationConfig)
	if err != nil {
		return instance.Info{}, err
	}
	logFile, err := openDaemonLog(logPath)
	if err != nil {
		return instance.Info{}, err
	}
	defer logFile.Close()

	child := exec.Command(executable, "serve", "--launched-by", string(instance.ModeDaemon))
	child.Stdin = nil
	child.Stdout = logFile
	child.Stderr = logFile
	child.SysProcAttr = detachedAttributes()
	if err := child.Start(); err != nil {
		return instance.Info{}, fmt.Errorf("start %s: %w", executable, err)
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()

	address := cli.DialAddress(applicationConfig.Addr)
	deadline := time.Now().Add(daemonStartTimeout)
	for time.Now().Before(deadline) {
		select {
		case waitError := <-exited:
			return instance.Info{}, startFailure(logPath, waitError)
		case <-time.After(daemonPollInterval):
		}
		info, held, _ := instance.Probe(lockPath)
		if held && info.PID == child.Process.Pid && answersHTTP(address) {
			return info, nil
		}
	}
	return instance.Info{}, fmt.Errorf("daemon (pid %d) did not answer on %s within %s; see %s",
		child.Process.Pid, applicationConfig.Addr, daemonStartTimeout, logPath)
}

func startFailure(logPath string, waitError error) error {
	reason := "exited"
	if waitError != nil {
		reason = waitError.Error()
	}
	tail, err := cli.TailLines(logPath, daemonLogTailLines)
	if err != nil || strings.TrimSpace(tail) == "" {
		return fmt.Errorf("daemon %s during start; see %s", reason, logPath)
	}
	return fmt.Errorf("daemon %s during start:\n%s", reason, tail)
}

// stopInstance sends SIGTERM to the lock holder and waits for the lock to
// drop. The lock proves the process is alive, so the recorded pid is never
// stale. Short maintenance commands are not stopped.
func stopInstance(lockPath string, timeout time.Duration) (bool, instance.Info, error) {
	info, held, err := instance.Probe(lockPath)
	if err != nil {
		return false, instance.Info{}, err
	}
	if !held {
		return false, info, nil
	}
	if info.Mode == instance.ModeMaintenance {
		return false, info, fmt.Errorf("a maintenance command (pid %d) holds the database; wait for it to finish", info.PID)
	}
	if info.PID == 0 {
		return false, info, errors.New("the lock holder recorded no pid; try again")
	}
	if err := terminateProcess(info.PID); err != nil {
		return false, info, fmt.Errorf("stop pid %d: %w", info.PID, err)
	}
	deadline := time.Now().Add(timeout)
	for {
		if _, stillHeld, err := instance.Probe(lockPath); err != nil {
			return false, info, err
		} else if !stillHeld {
			return true, info, nil
		}
		if time.Now().After(deadline) {
			return false, info, fmt.Errorf("pid %d did not exit within %s", info.PID, timeout)
		}
		time.Sleep(2 * daemonPollInterval)
	}
}

// answersHTTP reports whether anything answers on address. Any HTTP
// response counts: the server is listening.
func answersHTTP(address string) bool {
	client := &http.Client{
		Timeout:       time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Get("http://" + address + "/api/llm-router/status")
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return true
}

// daemonLogPath names the file that keeps the stdout and stderr of the
// background process. It sits in the state directory and carries the
// database name, so a dev and a prod instance do not share it.
func daemonLogPath(applicationConfig *config.Config) (string, error) {
	stateDirectory, err := config.StateApplicationDirectory()
	if err != nil {
		return "", err
	}
	name := filepath.Base(applicationConfig.DBPath)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	logPath := filepath.Join(stateDirectory, name+".daemon.log")
	if err := config.EnsureParentDirectory(logPath); err != nil {
		return "", err
	}
	return logPath, nil
}

func openDaemonLog(path string) (*os.File, error) {
	if info, err := os.Stat(path); err == nil && info.Size() > daemonLogLimit {
		if err := os.Truncate(path, 0); err != nil {
			return nil, fmt.Errorf("truncate %s: %w", path, err)
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return file, nil
}
