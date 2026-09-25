//go:build windows

package shared

import (
	"syscall"
	"testing"
)

// Forbidden flag values, locked here so the lesson survives: each was
// tried and each regressed differently. See newConsoleAttr.
const (
	forbiddenDetached = 0x00000008 // DETACHED_PROCESS
	forbiddenNoWindow = 0x08000000 // CREATE_NO_WINDOW
)

// The spawn attributes must survive refactors as a whole. Every subset
// regresses differently: group-only dies with the terminal, detached
// without a console spawns a fresh visible window for bun/node, and a
// hidden window without a new console stops at re-exec shims.
func TestNewConsoleAttrSurvivesTerminalClose(t *testing.T) {
	attr := newConsoleAttr()
	if attr == nil {
		t.Fatal("newConsoleAttr is nil")
	}
	if attr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Error("missing CREATE_NEW_PROCESS_GROUP")
	}
	if attr.CreationFlags&createNewConsole == 0 {
		t.Error("missing CREATE_NEW_CONSOLE: child shares the terminal console and dies with it")
	}
	if !attr.HideWindow {
		t.Error("missing HideWindow: the fresh console flashes visible")
	}
	if attr.CreationFlags&forbiddenDetached != 0 {
		t.Error("DETACHED_PROCESS must stay out: detached console apps get a fresh visible console")
	}
	if attr.CreationFlags&forbiddenNoWindow != 0 {
		t.Error("CREATE_NO_WINDOW must stay out: it stops at re-exec shims, grandchildren allocate visible consoles")
	}
}
