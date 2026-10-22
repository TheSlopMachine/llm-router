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
	"github.com/TheSlopMachine/llm-router/internal/i18n"
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
		Use:     "daemon",
		Short:   i18n.T("daemon.short"),
		Long:    i18n.T("daemon.long"),
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
			Short: i18n.T("daemon.start.short"),
			Args:  cobra.NoArgs,
			RunE:  runDaemonStart,
		},
		&cobra.Command{
			Use:   "stop",
			Short: i18n.T("daemon.stop.short"),
			Args:  cobra.NoArgs,
			RunE:  runDaemonStop,
		},
		&cobra.Command{
			Use:   "restart",
			Short: i18n.T("daemon.restart.short"),
			Args:  cobra.NoArgs,
			RunE:  runDaemonRestart,
		},
		&cobra.Command{
			Use:   "status",
			Short: i18n.T("daemon.status.short"),
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
		return errors.New(i18n.T("daemon.unsupported", runtime.GOOS))
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
		return i18n.Wrap(err, "daemon.resolve")
	}
	info, err := startDaemon(applicationConfig, executable)
	if err != nil {
		return err
	}
	fmt.Println(i18n.T("daemon.started", info.PID, applicationConfig.Addr))
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
		fmt.Println(i18n.T("daemon.notRunning"))
		return nil
	}
	fmt.Println(i18n.T("daemon.stopped", info.PID))
	return nil
}

func runDaemonRestart(*cobra.Command, []string) error {
	applicationConfig, _, err := config.Load()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return i18n.Wrap(err, "daemon.resolve")
	}
	if stopped, info, err := stopInstance(lockPathFor(applicationConfig), daemonStopTimeout); err != nil {
		return err
	} else if stopped {
		fmt.Println(i18n.T("daemon.stopped", info.PID))
	}
	info, err := startDaemon(applicationConfig, executable)
	if err != nil {
		return err
	}
	fmt.Println(i18n.T("daemon.started", info.PID, applicationConfig.Addr))
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
		return instance.Info{}, i18n.Wrap(err, "daemon.start", executable)
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
	return instance.Info{}, errors.New(i18n.T("daemon.startTimeout",
		child.Process.Pid, applicationConfig.Addr, daemonStartTimeout, logPath))
}

func startFailure(logPath string, waitError error) error {
	reason := i18n.T("daemon.exited")
	if waitError != nil {
		reason = waitError.Error()
	}
	tail, err := cli.TailLines(logPath, daemonLogTailLines)
	if err != nil || strings.TrimSpace(tail) == "" {
		return errors.New(i18n.T("daemon.startFailedLog", reason, logPath))
	}
	return errors.New(i18n.T("daemon.startFailed", reason, tail))
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
		return false, info, errors.New(i18n.T("daemon.maintenance", info.PID))
	}
	if info.PID == 0 {
		return false, info, errors.New(i18n.T("daemon.noPID"))
	}
	if err := terminateProcess(info.PID); err != nil {
		return false, info, i18n.Wrap(err, "daemon.stopPID", info.PID)
	}
	deadline := time.Now().Add(timeout)
	for {
		if _, stillHeld, err := instance.Probe(lockPath); err != nil {
			return false, info, err
		} else if !stillHeld {
			return true, info, nil
		}
		if time.Now().After(deadline) {
			return false, info, errors.New(i18n.T("daemon.didNotExit", info.PID, timeout))
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
