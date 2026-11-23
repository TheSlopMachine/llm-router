package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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
	// is killed.
	daemonStopTimeout = 5 * time.Minute
	// daemonKillWait bounds the wait for the kernel to end a killed process.
	daemonKillWait = 10 * time.Second
	// daemonNoticeDelay is the wait after which start and stop print what
	// they wait for.
	daemonNoticeDelay  = 2 * time.Second
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
		RunE: func(command *cobra.Command, _ []string) error {
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

// startDaemon launches "executable serve" detached and waits for it to
// start (awaitStart).
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
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	return awaitStart(child.Process, exited, interrupts, lockPath, logPath)
}

// awaitStart waits until the child holds the instance lock, then for the
// ready mark in the lock record. A child that exits during either wait
// fails the start with the tail of its output. Ctrl+C, or no lock within
// daemonLockTimeout, stops the child (abortStart): a failed start leaves
// no server behind. A child that holds the lock but is not ready within
// daemonReadyTimeout is started, not ready.
func awaitStart(process *os.Process, exited <-chan error, interrupts <-chan os.Signal, lockPath, logPath string) (daemonStart, error) {
	started := daemonStart{LogPath: logPath}
	launched := time.Now()
	noticed := false
	for {
		select {
		case waitError := <-exited:
			return daemonStart{}, startFailure(logPath, waitError)
		case <-interrupts:
			if err := abortStart(process, exited, interrupts); err != nil {
				return daemonStart{}, err
			}
			return daemonStart{}, errors.New(i18n.T("daemon.aborted", process.Pid))
		case <-time.After(daemonPollInterval):
		}
		if info, held, _ := instance.Probe(lockPath); held && info.PID == process.Pid {
			started.Info = info
			if info.Ready {
				started.Ready = true
				return started, nil
			}
		}
		if !noticed && time.Since(launched) >= daemonNoticeDelay {
			noticed = true
			fmt.Println(i18n.T("daemon.waitingReady", process.Pid))
		}
		if started.Info.PID == 0 {
			if time.Since(launched) >= daemonLockTimeout {
				timeout := errors.New(i18n.T("daemon.startTimeout", process.Pid, daemonLockTimeout, logPath))
				return daemonStart{}, errors.Join(timeout, abortStart(process, exited, interrupts))
			}
			continue
		}
		if time.Since(launched) >= daemonReadyTimeout {
			return started, nil
		}
	}
}

// abortStart stops a child that did not finish starting and waits for it
// to exit. It sends a stop request first. A failed request, a second
// Ctrl+C or daemonStopTimeout kills the child.
func abortStart(process *os.Process, exited <-chan error, interrupts <-chan os.Signal) error {
	fmt.Println(i18n.T("daemon.aborting", process.Pid))
	if err := requestStop(process.Pid); err != nil {
		fmt.Println(i18n.T("daemon.stopRequestFailed", process.Pid, err))
	} else {
		select {
		case <-exited:
			return nil
		case <-interrupts:
		case <-time.After(daemonStopTimeout):
			fmt.Println(i18n.T("daemon.killing", process.Pid, daemonStopTimeout))
		}
	}
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return i18n.Wrap(err, "daemon.stopPID", process.Pid)
	}
	select {
	case <-exited:
		return nil
	case <-time.After(daemonKillWait):
		return errors.New(i18n.T("daemon.didNotExit", process.Pid, daemonKillWait))
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

// stopInstance asks the lock holder to stop (requestStop) and waits for the
// lock to drop: the kernel drops it when the process exits. The lock proves
// the process is alive, so the recorded pid is never stale. Short
// maintenance commands are not stopped.
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
	if err := requestStop(info.PID); err != nil {
		return false, info, i18n.Wrap(err, "daemon.stopPID", info.PID)
	}
	notice := func() { fmt.Println(i18n.T("daemon.waitingExit", info.PID)) }
	err = waitForRelease(lockPath, timeout, daemonNoticeDelay, notice)
	if !errors.Is(err, errLockStillHeld) {
		return err == nil, info, err
	}
	// A process that ignores the stop request past the timeout is hung:
	// killProcess ends it. SQLite recovers its write-ahead log on the next
	// open.
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

// killProcess ends process pid at once: SIGKILL, or TerminateProcess on
// Windows.
func killProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	defer process.Release()
	return process.Kill()
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
