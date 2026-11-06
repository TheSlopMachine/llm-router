package cmd

import (
	"errors"
	"fmt"
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
	// daemonLockTimeout bounds the wait for the child to take the instance
	// lock: it does so right after loading the configuration.
	daemonLockTimeout = 20 * time.Second
	// daemonReadyTimeout bounds the wait for the ready mark in the lock
	// record: the server sets it once its listeners are bound, after the
	// startup plugin jobs. A child that holds the lock past this wait counts
	// as started.
	daemonReadyTimeout = 2 * time.Minute
	// daemonStopTimeout bounds a graceful shutdown: plugin hooks, listener
	// and events drains, database vacuum. A process still running after it
	// gets SIGKILL.
	daemonStopTimeout = 5 * time.Minute
	// daemonKillWait bounds the wait for the kernel to end a killed process.
	daemonKillWait     = 10 * time.Second
	daemonStopNotice   = 2 * time.Second
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
	started, err := startDaemon(applicationConfig, executable)
	if err != nil {
		return err
	}
	printDaemonStarted(started, applicationConfig.Addr)
	return nil
}

// printDaemonStarted reports a started daemon, ready or still starting.
func printDaemonStarted(started daemonStart, address string) {
	if started.Ready {
		fmt.Println(i18n.T("daemon.started", started.Info.PID, address))
		return
	}
	fmt.Println(i18n.T("daemon.starting", started.Info.PID, started.LogPath))
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
	started, err := startDaemon(applicationConfig, executable)
	if err != nil {
		return err
	}
	printDaemonStarted(started, applicationConfig.Addr)
	return nil
}

// daemonStart describes a started background server. Ready reports that
// it marked itself ready within daemonReadyTimeout.
type daemonStart struct {
	Info    instance.Info
	Ready   bool
	LogPath string
}

// startDaemon launches "executable serve" detached, waits until the child
// holds the instance lock, then waits for the ready mark in the lock
// record. A child that exits during either wait fails the start with the
// tail of its output. A child that holds the lock but is not ready within
// daemonReadyTimeout is started, not ready.
func startDaemon(applicationConfig *config.Config, executable string) (daemonStart, error) {
	lockPath := lockPathFor(applicationConfig)
	if info, held, err := instance.Probe(lockPath); err != nil {
		return daemonStart{}, err
	} else if held {
		return daemonStart{}, &instance.HeldError{Info: info}
	}
	logPath, err := daemonLogPath(applicationConfig)
	if err != nil {
		return daemonStart{}, err
	}
	logFile, err := openDaemonLog(logPath)
	if err != nil {
		return daemonStart{}, err
	}
	defer logFile.Close()

	child := exec.Command(executable, "serve", "--launched-by", string(instance.ModeDaemon))
	child.Stdin = nil
	child.Stdout = logFile
	child.Stderr = logFile
	child.SysProcAttr = detachedAttributes()
	if err := child.Start(); err != nil {
		return daemonStart{}, i18n.Wrap(err, "daemon.start", executable)
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()

	started := daemonStart{LogPath: logPath}
	launched := time.Now()
	for {
		select {
		case waitError := <-exited:
			return daemonStart{}, startFailure(logPath, waitError)
		case <-time.After(daemonPollInterval):
		}
		if info, held, _ := instance.Probe(lockPath); held && info.PID == child.Process.Pid {
			started.Info = info
			if info.Ready {
				started.Ready = true
				return started, nil
			}
		}
		if started.Info.PID == 0 {
			if time.Since(launched) >= daemonLockTimeout {
				return daemonStart{}, errors.New(i18n.T("daemon.startTimeout",
					child.Process.Pid, daemonLockTimeout, logPath))
			}
			continue
		}
		if time.Since(launched) >= daemonReadyTimeout {
			return started, nil
		}
	}
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
// drop: the kernel drops it when the process exits. The lock proves the
// process is alive, so the recorded pid is never stale. Short maintenance
// commands are not stopped.
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
	notice := func() { fmt.Println(i18n.T("daemon.waitingExit", info.PID)) }
	err = waitForRelease(lockPath, timeout, daemonStopNotice, notice)
	if !errors.Is(err, errLockStillHeld) {
		return err == nil, info, err
	}
	// A process that ignores SIGTERM past the timeout is hung: SIGKILL
	// ends it. SQLite recovers its write-ahead log on the next open.
	fmt.Println(i18n.T("daemon.killing", info.PID, timeout))
	if err := killProcess(info.PID); err != nil {
		return false, info, i18n.Wrap(err, "daemon.stopPID", info.PID)
	}
	err = waitForRelease(lockPath, daemonKillWait, daemonKillWait, func() {})
	if errors.Is(err, errLockStillHeld) {
		return false, info, errors.New(i18n.T("daemon.didNotExit", info.PID, timeout+daemonKillWait))
	}
	return err == nil, info, err
}

// errLockStillHeld reports a lock that waitForRelease saw held to the end.
var errLockStillHeld = errors.New("lock still held")

// waitForRelease polls until nobody holds the lock at lockPath. It calls
// notice once when the wait passes noticeAfter.
func waitForRelease(lockPath string, timeout, noticeAfter time.Duration, notice func()) error {
	began := time.Now()
	noticed := false
	for {
		_, held, err := instance.Probe(lockPath)
		if err != nil {
			return err
		}
		if !held {
			return nil
		}
		elapsed := time.Since(began)
		if elapsed >= timeout {
			return errLockStillHeld
		}
		if !noticed && elapsed >= noticeAfter {
			noticed = true
			notice()
		}
		time.Sleep(2 * daemonPollInterval)
	}
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
