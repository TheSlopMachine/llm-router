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
	// Vet both Go modules: the root module and scripts/ itself, mirroring
	// what fmt covers. A scripts-only breakage must fail the gate too.
	shared.Stepf("Running go vet...")
	for _, dir := range []string{root, filepath.Join(root, "scripts")} {
		cmd := exec.Command("go", "vet", "./...")
		cmd.Dir = dir
		cmd.Env = shared.EnvWithoutGowork()
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			shared.Failf("go vet (%s): %v", dir, err)
		}
	}
	shared.OKf("go vet passed")
}
