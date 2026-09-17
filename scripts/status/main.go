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
	p, err := shared.ReadPidFile(pidFile)
	if err != nil {
		shared.Failf("parse pidfile %s: %v", pidFile, err)
	}

	report := func(name string, pid int) {
		if pid <= 0 {
			fmt.Printf("%s: not recorded\n", name)
			return
		}
		if shared.Alive(pid) {
			fmt.Printf("%s is running as PID %d (%s)\n", name, pid, shared.ProcessPath(pid))
		} else {
			fmt.Printf("%s PID %d is not running\n", name, pid)
		}
	}
	report("backend", p.Backend)
	report("frontend", p.Frontend)

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
	reportPort("frontend port", p.VitePort)
	reportPort("backend api port", mustAtoiSoft(shared.Getenv("API_PORT", "8081")))
	reportPort("backend internal port", mustAtoiSoft(devBackendWebPort))

	fmt.Printf("Backend log: %s\n", shared.DefaultBackendLog())
	fmt.Printf("Frontend log: %s\n", shared.DefaultFrontendLog())
}

func mustAtoiSoft(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
