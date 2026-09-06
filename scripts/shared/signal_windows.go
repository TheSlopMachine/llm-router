//go:build windows

package shared

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
)

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess = kernel32.NewProc("OpenProcess")
	procCloseHandle = kernel32.NewProc("CloseHandle")
)

const processQueryLimitedInformation = 0x1000

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

// terminate asks pid to exit via taskkill without /F (graceful close
// request) and without /T (only the recorded process, never a tree kill).
func terminate(pid int) error {
	out, err := exec.Command("taskkill", "/PID", strconv.Itoa(pid)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("taskkill /PID %d: %w: %s", pid, err, string(out))
	}
	return nil
}
