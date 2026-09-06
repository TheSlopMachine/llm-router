//go:build windows

package shared

import (
	"fmt"
	"syscall"
)

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess              = kernel32.NewProc("OpenProcess")
	procCloseHandle              = kernel32.NewProc("CloseHandle")
	procGenerateConsoleCtrlEvent = kernel32.NewProc("GenerateConsoleCtrlEvent")
)

const (
	processQueryLimitedInformation = 0x1000
	ctrlBreakEvent                 = 1 // CTRL_BREAK_EVENT
)

// Alive reports whether pid exists by probing it with OpenProcess.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, _, _ := procOpenProcess.Call(uintptr(processQueryLimitedInformation), 0, uintptr(pid))
	if h == 0 {
		return false
	}
	procCloseHandle.Call(h)
	return true
}

// terminate asks pid to shut down gracefully by sending CTRL_BREAK_EVENT to
// its process group. Windows has no SIGTERM equivalent that can be sent to
// an arbitrary process, but SpawnDetached creates children with
// CREATE_NEW_PROCESS_GROUP, which makes the child's own PID double as its
// process group ID. GenerateConsoleCtrlEvent can then target that group
// without also signaling the caller (a different group) or anything else
// sharing the console.
//
// The Go runtime's own console-control handler turns a received
// CTRL_BREAK_EVENT into a regular os.Interrupt, exactly like signal.Notify
// receiving SIGINT/SIGTERM on unix. A target process that calls
// signal.Notify(ch, os.Interrupt) gets a chance to shut down cleanly; one
// that doesn't is terminated by the default OS handler. Either way this
// never force-kills via TerminateProcess itself — if the process survives,
// that is the caller's problem (see stop's grace-period/exit-1 behavior).
//
// This requires the calling process to have an attached console shared with
// the target's console session (true for a normal terminal invocation of
// `make stop`). If there is no such console, the call fails and terminate
// returns an error rather than silently falling back to a force-kill.
func terminate(pid int) error {
	ok, _, err := procGenerateConsoleCtrlEvent.Call(uintptr(ctrlBreakEvent), uintptr(pid))
	if ok == 0 {
		return fmt.Errorf("GenerateConsoleCtrlEvent PID %d: %w", pid, err)
	}
	return nil
}
