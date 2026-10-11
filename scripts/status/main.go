package main

import (
	"fmt"
	"os"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

const devBackendWebPort = "38473"

func main() {
	pidFile := shared.Getenv("PID_FILE", shared.DefaultPidFile())

	if _, err := os.Stat(pidFile); os.IsNotExist(err) {
		fmt.Println("llm-router dev server is not running")
		fmt.Printf("Backend log: %s\n", shared.DefaultBackendLog())
		fmt.Printf("Frontend log: %s\n", shared.DefaultFrontendLog())
		return
	}
	process, err := shared.ReadPidFile(pidFile)
	if err != nil {
		shared.Failf("parse pidfile %s: %v", pidFile, err)
	}

	report := func(name string, pid int, expectedPath string) {
		if pid <= 0 {
			fmt.Printf("%s: not recorded\n", name)
			return
		}
		actual := shared.ProcessPath(pid)
		if shared.AliveMatches(pid, expectedPath) {
			fmt.Printf("%s is running as PID %d (%s)\n", name, pid, actual)
			return
		}
		if shared.Alive(pid) {
			fmt.Printf("%s PID %d is stale\n", name, pid)
			return
		}
		fmt.Printf("%s PID %d is not running\n", name, pid)
	}
	report("backend", process.Backend, process.BackendPath)
	report("frontend", process.Frontend, process.FrontendPath)

	reportPort := func(label string, port int) {
		if port <= 0 {
			return
		}
		if free, holder := shared.PortStatus(port); free {
			fmt.Printf("%s (:%d): free\n", label, port)
		} else {
			fmt.Printf("%s (:%d): held by %s\n", label, port, holder)
		}
	}
	reportPort("frontend port", process.VitePort)
	reportPort("backend api port", mustAtoiSoft(shared.Getenv("API_PORT", "38081")))
	reportPort("backend internal port", mustAtoiSoft(devBackendWebPort))

	fmt.Printf("Backend log: %s\n", shared.DefaultBackendLog())
	fmt.Printf("Frontend log: %s\n", shared.DefaultFrontendLog())
}

func mustAtoiSoft(portString string) int {
	number := 0
	for _, digit := range portString {
		if digit < '0' || digit > '9' {
			return 0
		}
		number = number*10 + int(digit-'0')
	}
	return number
}
