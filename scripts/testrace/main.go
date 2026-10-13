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
	directory, pattern, scoped := shared.ResolveScope(root, packagePattern)
	commandDir := root
	arguments := []string{"test", "-race", packagePattern}
	environment := shared.EnvWithoutGowork()
	if scoped {
		commandDir = directory
		arguments = []string{"test", "-race", pattern}
		if commandDir != root {
			environment = shared.EnvWith([]string{"GOWORK=off"})
		}
	}
	cmd := exec.Command("go", arguments...)
	cmd.Dir = commandDir
	cmd.Env = environment
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		shared.Failf("go test -race: %v", err)
	}
	shared.OKf("go test -race passed")
}
