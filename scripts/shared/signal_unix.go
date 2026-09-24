//go:build !windows

package shared

import (
	"fmt"
	"os"
	"syscall"
)

// Alive reports whether pid exists. Signal 0 performs error checking
// without delivering a signal.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// terminate sends SIGTERM to pid's process group, not just pid itself.
// detachedAttr's Setpgid made the spawned process (e.g. `bun run dev`) the
// leader of its own group, with any child it forks (vite, esbuild) inheriting
// that pgid automatically. A plain Kill(pid, ...) only reaches the group
// leader: if that leader exits on SIGTERM without forwarding the signal to
// its own children (which is common -- bun/npm/node script runners do not
// all propagate signals to subprocesses), those children are orphaned.
// Alive(pid) then reports the leader dead, `stop` declares success and
// deletes the pidfile, and the orphan -- holding no listening port, so
// `start`'s port check never catches it -- keeps running invisibly. Repeated
// restart cycles accumulate one such orphan each time, which is exactly the
// slow, restart-driven memory growth this was causing: killing the group
// here, matching forceKillAll below and the Windows terminate (which is
// group-aware via CREATE_NEW_PROCESS_GROUP), reaches descendants on the
// graceful path too, so they no longer depend on the force-kill fallback
// ever triggering.
func terminate(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGTERM)
	if err != nil && err == syscall.ESRCH {
		// No such process group -- e.g. a pre-fix pidfile recorded a pid that
		// was never made a group leader. Fall back to the single pid so an
		// old pidfile does not turn stop into a hard failure.
		return syscall.Kill(pid, syscall.SIGTERM)
	}
	return err
}

// registerSession is a no-op on unix. detachedAttr already sets Setpgid,
// making the spawned process the leader of its own new process group;
// anything it forks inherits that pgid automatically, so forceKillAll's
// -pid signal already reaches the whole tree with no extra bookkeeping.
func registerSession(pidFile string, pids []int) error { return nil }

// forceKillAll sends SIGKILL to each pid's process group. Missing
// processes stay silent so a PID that exits mid-stop counts as success.
func forceKillAll(pidFile string, pids []int) error {
	var firstErr error
	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		if !Alive(pid) {
			continue
		}
		if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
			if !Alive(pid) {
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// processPath resolves pid's executable path via /proc, best-effort. Works
// on Linux; returns "unknown" on platforms without /proc (e.g. macOS) or on
// any other failure.
func processPath(pid int) string {
	link, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return "unknown"
	}
	return link
}
