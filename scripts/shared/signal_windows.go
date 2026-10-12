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
	procTerminateProcess           = kernel32.NewProc("TerminateProcess")
	procCreateJobObjectW           = kernel32.NewProc("CreateJobObjectW")
	procOpenJobObjectW             = kernel32.NewProc("OpenJobObjectW")
	procAssignProcessToJobObject   = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject         = kernel32.NewProc("TerminateJobObject")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	procGetCurrentProcess          = kernel32.NewProc("GetCurrentProcess")
	procDuplicateHandle            = kernel32.NewProc("DuplicateHandle")
	procGetExitCodeProcess         = kernel32.NewProc("GetExitCodeProcess")
)

const (
	stillActive = 259 // STILL_ACTIVE from GetExitCodeProcess
)

const (
	processQueryLimitedInformation = 0x1000
	processSetQuota                = 0x0100 // required to AssignProcessToJobObject
	processTerminateRight          = 0x0001 // required to AssignProcessToJobObject / TerminateProcess
	processDupHandle               = 0x0040 // required to DuplicateHandle a job handle into another process
	jobObjectTerminateRight        = 0x0008 // JOB_OBJECT_TERMINATE
	duplicateSameAccess            = 0x0002 // DUPLICATE_SAME_ACCESS
)

// Alive reports whether pid is still running. OpenProcess alone succeeds
// for an exited process while handles to it stay open elsewhere, so the
// exit code decides. An undeterminable exit code stays conservative.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, _, _ := procOpenProcess.Call(uintptr(processQueryLimitedInformation), 0, uintptr(pid))
	if h == 0 {
		return false
	}
	defer procCloseHandle.Call(h)
	var code uint32
	ok, _, _ := procGetExitCodeProcess.Call(h, uintptr(unsafe.Pointer(&code)))
	if ok == 0 {
		return true
	}
	return code == stillActive
}

// terminate always returns ErrGracefulUnsupported. Windows stops through
// the job-object force path, which has no console-sharing requirement.
func terminate(int) error {
	return ErrGracefulUnsupported
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
	jobHandle, _, jerr := procCreateJobObjectW.Call(0, uintptr(unsafe.Pointer(namePtr)))
	if jobHandle == 0 {
		return fmt.Errorf("CreateJobObjectW: %w", jerr)
	}
	defer procCloseHandle.Call(jobHandle)

	currentProcess, _, _ := procGetCurrentProcess.Call() // pseudo-handle, no cleanup needed

	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		processHandle, _, perr := procOpenProcess.Call(
			uintptr(processSetQuota|processTerminateRight|processDupHandle), 0, uintptr(pid))
		if processHandle == 0 {
			return fmt.Errorf("OpenProcess PID %d: %w", pid, perr)
		}

		if ok, _, aerr := procAssignProcessToJobObject.Call(jobHandle, processHandle); ok == 0 {
			procCloseHandle.Call(processHandle)
			return fmt.Errorf("AssignProcessToJobObject PID %d: %w", pid, aerr)
		}

		var duplicatedHandle uintptr
		ok, _, derr := procDuplicateHandle.Call(
			currentProcess, jobHandle, processHandle, uintptr(unsafe.Pointer(&duplicatedHandle)), 0, 0, duplicateSameAccess)
		procCloseHandle.Call(processHandle)
		if ok == 0 {
			return fmt.Errorf("DuplicateHandle pin job to PID %d: %w", pid, derr)
		}
		// duplicatedHandle now lives in pid's handle table. Never closed here.
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
	jobHandle, _, _ := procOpenJobObjectW.Call(uintptr(jobObjectTerminateRight), 0, uintptr(unsafe.Pointer(namePtr)))
	if jobHandle == 0 {
		return forceKillFallback(pids)
	}
	defer procCloseHandle.Call(jobHandle)

	ok, _, terr := procTerminateJobObject.Call(jobHandle, 1)
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
		if !Alive(pid) {
			continue
		}
		h, _, _ := procOpenProcess.Call(uintptr(processTerminateRight), 0, uintptr(pid))
		if h == 0 {
			if !Alive(pid) {
				continue
			}
			continue
		}
		ok, _, terr := procTerminateProcess.Call(h, 1)
		procCloseHandle.Call(h)
		if ok == 0 {
			if !Alive(pid) {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("TerminateProcess PID %d: %w", pid, terr)
			}
		}
	}
	return firstErr
}

// processPath resolves pid's executable path, best-effort. Returns
// "unknown" on any failure.
func processPath(pid int) string {
	processHandle, _, _ := procOpenProcess.Call(uintptr(processQueryLimitedInformation), 0, uintptr(pid))
	if processHandle == 0 {
		return "unknown"
	}
	defer procCloseHandle.Call(processHandle)
	// 32K chars: diagnostic-only and cheap, and Windows paths exceed 260.
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	ok, _, _ := procQueryFullProcessImageNameW.Call(processHandle, 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return "unknown"
	}
	return syscall.UTF16ToString(buffer[:size])
}
