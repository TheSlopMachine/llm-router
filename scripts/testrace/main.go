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
	packagePattern := shared.Getenv("PKG", "./...")
	shared.Stepf("Running go test -race (PKG=%s)...", packagePattern)
	cmd := exec.Command("go", "test", "-race", packagePattern)
	cmd.Dir = root
	cmd.Env = shared.EnvWithoutGowork()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		shared.Failf("go test -race: %v", err)
	}
	shared.OKf("go test -race passed")
}
