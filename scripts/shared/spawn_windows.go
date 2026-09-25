//go:build windows

package shared

import "syscall"

// createNewConsole is CREATE_NEW_CONSOLE (0x10): the child gets a console
// of its own instead of sharing the terminal's. Absent from Go's syscall
// package.
const createNewConsole = 0x00000010

// newConsoleAttr builds process attributes for daemon spawns: the child
// gets a fresh console, created hidden, in its own Ctrl+C/Break group.
//
// Every subset of this regresses, each in its own way:
//   - group only: child shares the terminal console and dies with it via
//     CTRL_CLOSE_EVENT;
//   - detached without a console: Windows allocates a brand-new VISIBLE
//     console to console apps (bun, node) that find none to inherit;
//   - hidden window without a new console: hides only our direct child,
//     while re-exec shims (scoop) spawn visible grandchildren.
//
// A separate hidden console fixes the whole tree at once: grandchildren
// inherit invisibility through any number of shim layers, terminal close
// signals a console the daemons don't belong to, and nothing flashes
// because the console is born hidden, not shown-then-hidden.
// Stdout/stderr already go to log files. Graceful stop via console events
// stays best-effort (see terminate); force-kill uses job objects, which
// are console-independent.
func newConsoleAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | createNewConsole,
		HideWindow:    true,
	}
}
