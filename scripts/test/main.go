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
	pkg := shared.Getenv("PKG", "./...")
	shared.Stepf("Running go test (PKG=%s)...", pkg)
	cmd := exec.Command("go", "test", pkg)
	cmd.Dir = root
	cmd.Env = shared.EnvWithoutGowork()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		shared.Failf("go test: %v", err)
	}
	shared.OKf("go test passed")
}
