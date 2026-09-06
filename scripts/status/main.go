package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

func main() {
	pidFile := flag.String("pid-file", shared.DefaultPidFile(), "path to pidfile")
	flag.Parse()

	if _, err := os.Stat(*pidFile); os.IsNotExist(err) {
		fmt.Println("llm-router dev server is not running")
		fmt.Printf("Backend log: %s\n", shared.DefaultBackendLog())
		fmt.Printf("Frontend log: %s\n", shared.DefaultFrontendLog())
		return
	}
	p, err := shared.ReadPidFile(*pidFile)
	if err != nil {
		shared.Failf("parse pidfile %s: %v", *pidFile, err)
	}
	report := func(name string, pid int) {
		if pid <= 0 {
			fmt.Printf("%s: not recorded\n", name)
			return
		}
		if shared.Alive(pid) {
			fmt.Printf("%s is running as PID %d\n", name, pid)
		} else {
			fmt.Printf("%s PID %d is not running\n", name, pid)
		}
	}
	report("backend", p.Backend)
	report("frontend", p.Frontend)
	fmt.Printf("Backend log: %s\n", shared.DefaultBackendLog())
	fmt.Printf("Frontend log: %s\n", shared.DefaultFrontendLog())
}
