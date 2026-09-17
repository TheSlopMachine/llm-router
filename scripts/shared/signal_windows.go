//go:build windows

package shared

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess                = kernel32.NewProc("OpenProcess")
	procCloseHandle                = kernel32.NewProc("CloseHandle")
	procGenerateConsoleCtrlEvent   = kernel32.NewProc("GenerateConsoleCtrlEvent")
	procTerminateProcess           = kernel32.NewProc("TerminateProcess")
	procCreateJobObjectW           = kernel32.NewProc("CreateJobObjectW")
	procOpenJobObjectW             = kernel32.NewProc("OpenJobObjectW")
	procAssignProcessToJobObject   = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject         = kernel32.NewProc("TerminateJobObject")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	procGetCurrentProcess          = kernel32.NewProc("GetCurrentProcess")
	procDuplicateHandle            = kernel32.NewProc("DuplicateHandle")
)

const (
	processQueryLimitedInformation = 0x1000
	processSetQuota                = 0x0100 // required to AssignProcessToJobObject
	processTerminateRight          = 0x0001 // required to AssignProcessToJobObject / TerminateProcess
	processDupHandle               = 0x0040 // required to DuplicateHandle a job handle into another process
	ctrlBreakEvent                 = 1      // CTRL_BREAK_EVENT
	jobObjectTerminateRight        = 0x0008 // JOB_OBJECT_TERMINATE
	duplicateSameAccess            = 0x0002 // DUPLICATE_SAME_ACCESS
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

// terminate asks pid to shut down gracefully via CTRL_BREAK_EVENT to its
// process group (see spawn_windows.go: CREATE_NEW_PROCESS_GROUP makes the
// child's PID double as its group ID). This fails with
// ERROR_INVALID_PARAMETER whenever the caller shares no console session
// with the target group - e.g. `stop` running in a different terminal tab
// than the `start` that spawned it - independent of whether the target
// handles the signal. Callers fall through to the force path on failure.
func terminate(pid int) error {
	ok, _, err := procGenerateConsoleCtrlEvent.Call(uintptr(ctrlBreakEvent), uintptr(pid))
	if ok == 0 {
		return fmt.Errorf("GenerateConsoleCtrlEvent PID %d: %w", pid, err)
	}
	return nil
}

// jobObjectName derives a stable Windows kernel-object name from pidFile,
// so a `start` and a later, unrelated `stop` process agree on the same job
// object with no shared state beyond the pidfile path. Opening a named job
// object has no console-sharing requirement.
func jobObjectName(pidFile string) string {
	sum := sha256.Sum256([]byte(pidFile))
	// Full 64-char hex hash: CreateJobObjectW/OpenJobObjectW allow names up
	// to MAX_PATH, and `Local\llm-router-dev-<64 hex chars>` stays under it.
	return `Local\llm-router-dev-` + hex.EncodeToString(sum[:])
}

// registerSession creates (or reopens) pidFile's job object, assigns pids
// to it, and duplicates the job handle into each pid's own handle table.
//
// Windows unregisters a named kernel object's name from the object
// namespace as soon as its last handle closes, even while the object body
// stays alive through other references such as assigned processes. Start's
// own handle closes the moment start exits, so without a handle living
// elsewhere, a later `stop` finds no job by name and degrades to the
// per-PID fallback, which misses descendants.
//
// The duplicated copies need no handling by the member processes. Windows
// reclaims each copy automatically when its process exits, which matches
// the intended lifetime pin from that member.
//
// Called once from start, right after spawning, so the window where a
// spawned process forks a grandchild before joining the job stays small.
func registerSession(pidFile string, pids []int) error {
	namePtr, err := syscall.UTF16PtrFromString(jobObjectName(pidFile))
	if err != nil {
		return fmt.Errorf("job object name: %w", err)
	}
	jh, _, jerr := procCreateJobObjectW.Call(0, uintptr(unsafe.Pointer(namePtr)))
	if jh == 0 {
		return fmt.Errorf("CreateJobObjectW: %w", jerr)
	}
	defer procCloseHandle.Call(jh)

	curProc, _, _ := procGetCurrentProcess.Call() // pseudo-handle, no cleanup needed

	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		ph, _, perr := procOpenProcess.Call(
			uintptr(processSetQuota|processTerminateRight|processDupHandle), 0, uintptr(pid))
		if ph == 0 {
			return fmt.Errorf("OpenProcess PID %d: %w", pid, perr)
		}

		if ok, _, aerr := procAssignProcessToJobObject.Call(jh, ph); ok == 0 {
			procCloseHandle.Call(ph)
			return fmt.Errorf("AssignProcessToJobObject PID %d: %w", pid, aerr)
		}

		var dup uintptr
		ok, _, derr := procDuplicateHandle.Call(
			curProc, jh, ph, uintptr(unsafe.Pointer(&dup)), 0, 0, duplicateSameAccess)
		procCloseHandle.Call(ph)
		if ok == 0 {
			return fmt.Errorf("DuplicateHandle pin job to PID %d: %w", pid, derr)
		}
		// dup now lives in pid's handle table. Never closed here.
	}
	return nil
}

// forceKillAll terminates every process assigned to pidFile's job object,
// including descendants never explicitly PID-tracked (e.g. a Vite/node
// grandchild under bun). Falls back to best-effort per-PID TerminateProcess
// when no job object exists: a pidfile written before jobs existed, or a
// skipped registration at start time.
func forceKillAll(pidFile string, pids []int) error {
	namePtr, err := syscall.UTF16PtrFromString(jobObjectName(pidFile))
	if err != nil {
		return fmt.Errorf("job object name: %w", err)
	}
	jh, _, _ := procOpenJobObjectW.Call(uintptr(jobObjectTerminateRight), 0, uintptr(unsafe.Pointer(namePtr)))
	if jh == 0 {
		return forceKillFallback(pids)
	}
	defer procCloseHandle.Call(jh)

	ok, _, terr := procTerminateJobObject.Call(jh, 1)
	if ok == 0 {
		return fmt.Errorf("TerminateJobObject: %w", terr)
	}
	return nil
}

func forceKillFallback(pids []int) error {
	var firstErr error
	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		h, _, _ := procOpenProcess.Call(uintptr(processTerminateRight), 0, uintptr(pid))
		if h == 0 {
			continue // already gone
		}
		if ok, _, terr := procTerminateProcess.Call(h, 1); ok == 0 && firstErr == nil {
			firstErr = fmt.Errorf("TerminateProcess PID %d: %w", pid, terr)
		}
		procCloseHandle.Call(h)
	}
	return firstErr
}

// processPath resolves pid's executable path, best-effort. Returns
// "unknown" on any failure. Diagnostic-only, never load-bearing.
func processPath(pid int) string {
	h, _, _ := procOpenProcess.Call(uintptr(processQueryLimitedInformation), 0, uintptr(pid))
	if h == 0 {
		return "unknown"
	}
	defer procCloseHandle.Call(h)
	// 32K chars: diagnostic-only and cheap, and Windows paths exceed 260.
	buf := make([]uint16, 32768)
	size := uint32(len(buf))
	ok, _, _ := procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return "unknown"
	}
	return syscall.UTF16ToString(buf[:size])
}
