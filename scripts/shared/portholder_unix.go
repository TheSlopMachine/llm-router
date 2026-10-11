//go:build !windows

package shared

import (
	"fmt"
	"os/exec"
	"strings"
)

func describePortHolder(port int) string {
	output, err := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port), "-sTCP:LISTEN", "-t").Output()
	if err != nil || len(strings.TrimSpace(string(output))) == 0 {
		return "unknown process"
	}
	pid := strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
	return fmt.Sprintf("PID %s", pid)
}
