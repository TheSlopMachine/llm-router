//go:build windows

package shared

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// describePortHolder shells out to netstat instead of pulling in the IP
// Helper API for a diagnostic-only message. Parse failure yields
// "unknown process".
func describePortHolder(port int) string {
	out, err := exec.Command("netstat", "-ano", "-p", "tcp").Output()
	if err != nil {
		return "unknown process (netstat failed)"
	}
	needle := fmt.Sprintf(":%d ", port)
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, needle) || !strings.Contains(line, "LISTENING") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if pid, err := strconv.Atoi(fields[len(fields)-1]); err == nil {
			return fmt.Sprintf("PID %d (%s)", pid, processPath(pid))
		}
	}
	return "unknown process"
}
