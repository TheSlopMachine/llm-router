//go:build windows

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/TheSlopMachine/llm-router/internal/i18n"
)

// detachedAttributes gives the child a hidden console of its own in a new
// process group. Closing the terminal or pressing Ctrl+C in it does not
// reach the child. Console programs the child starts open no window.
func detachedAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NEW_CONSOLE,
		HideWindow:    true,
	}
}

// stopEventName names the event that asks process pid to shut down.
// Windows has no SIGTERM. The Global namespace lets a stop from another
// logon session reach the process. The default security of the event
// admits only the same user and administrators.
func stopEventName(pid int) (*uint16, error) {
	return windows.UTF16PtrFromString(fmt.Sprintf(`Global\llm-router-stop-%d`, pid))
}

// requestStop sets the stop event of process pid.
func requestStop(pid int) error {
	name, err := stopEventName(pid)
	if err != nil {
		return err
	}
	event, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return errors.New(i18n.T("daemon.noStopEvent"))
	}
	if err != nil {
		return fmt.Errorf("open stop event: %w", err)
	}
	defer windows.CloseHandle(event)
	if err := windows.SetEvent(event); err != nil {
		return fmt.Errorf("set stop event: %w", err)
	}
	return nil
}

// shutdownContext ends on Ctrl+C, on SIGTERM (console close, logoff,
// system shutdown) and when requestStop sets the stop event of this
// process. An existing event of that name fails the call: another process
// could set it.
func shutdownContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	name, err := stopEventName(os.Getpid())
	if err != nil {
		return nil, nil, err
	}
	stopEvent, err := windows.CreateEvent(nil, 1, 0, name)
	if err != nil {
		if stopEvent != 0 {
			windows.CloseHandle(stopEvent)
		}
		return nil, nil, fmt.Errorf("create stop event: %w", err)
	}
	releaseEvent, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(stopEvent)
		return nil, nil, fmt.Errorf("create release event: %w", err)
	}
	signalContext, stopSignals := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(signalContext)
	waited := make(chan struct{})
	go func() {
		defer close(waited)
		index, err := windows.WaitForMultipleObjects(
			[]windows.Handle{stopEvent, releaseEvent}, false, windows.INFINITE)
		if err != nil {
			// A server that cannot receive stop requests shuts down.
			fmt.Fprintln(os.Stderr, "wait for stop event:", err)
			cancel()
			return
		}
		if index == windows.WAIT_OBJECT_0 {
			cancel()
		}
	}()
	release := func() {
		cancel()
		stopSignals()
		_ = windows.SetEvent(releaseEvent)
		<-waited
		windows.CloseHandle(stopEvent)
		windows.CloseHandle(releaseEvent)
	}
	return ctx, release, nil
}
