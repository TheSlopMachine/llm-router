package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

func main() {
	root, err := shared.RootDir()
	if err != nil {
		shared.Failf("%v", err)
	}
	shared.Stepf("Checking layout spacing...")
	violations, err := checkLayout(filepath.Join(root, "web", "src"))
	if err != nil {
		shared.Failf("layout check: %v", err)
	}
	for _, violation := range violations {
		fmt.Fprintln(os.Stderr, "web/src/"+violation.String())
	}
	if len(violations) > 0 {
		shared.Failf("layout check: %d lines place views by number", len(violations))
	}
	shared.OKf("layout check passed")
	shared.Stepf("Running svelte-check...")
	cmd := exec.Command("bun", "run", "check")
	cmd.Dir = filepath.Join(root, "web")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		shared.Failf("svelte-check: %v", err)
	}
	shared.OKf("svelte-check passed")
}
