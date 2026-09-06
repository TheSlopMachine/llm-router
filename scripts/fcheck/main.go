package main

import (
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
