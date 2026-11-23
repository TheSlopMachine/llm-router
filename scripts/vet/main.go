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
	packagePattern := shared.Getenv("PKG", "./...")
	targetOS := shared.Getenv("VET_GOOS", "")
	// Unscoped runs cover both Go modules; a scoped PKG narrows to exactly
	// the requested tree.
	directories := []string{root, filepath.Join(root, "scripts")}
	patterns := []string{"./...", "./..."}
	if directory, pattern, scoped := shared.ResolveScope(root, packagePattern); scoped {
		directories = []string{directory}
		patterns = []string{pattern}
	}
	for i, directory := range directories {
		shared.Stepf("Running go vet (%s)...", patterns[i])
		cmd := exec.Command("go", "vet", patterns[i])
		cmd.Dir = directory
		// The scripts module stays hermetic (never in go.work): vet it
		// with the workspace off. The root module vets inside it.
		var extra []string
		if directory != root {
			extra = append(extra, "GOWORK=off")
		}
		// VET_GOOS checks the files of another platform's build.
		if targetOS != "" {
			extra = append(extra, "GOOS="+targetOS)
		}
		cmd.Env = shared.EnvWith(extra)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			shared.Failf("go vet: %v", err)
		}
	}
	shared.OKf("go vet passed")
}
