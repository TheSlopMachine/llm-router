package main

import (
	"os"
	"os/exec"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

func main() {
	root, err := shared.RootDir()
	if err != nil {
		shared.Failf("%v", err)
	}
	shared.Stepf("Running go vet...")
	cmd := exec.Command("go", "vet", "./...")
	cmd.Dir = root
	cmd.Env = shared.EnvWithoutGowork()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		shared.Failf("go vet: %v", err)
	}
	shared.OKf("go vet passed")
}
