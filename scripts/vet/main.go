package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

func main() {
	root, err := shared.RootDir()
	if err != nil {
		shared.Failf("%v", err)
	}
	packagePattern := shared.Getenv("PKG", "./...")
	// Unscoped runs cover both Go modules; a scoped PKG narrows to exactly
	// the requested tree.
	directories := []string{root, filepath.Join(root, "scripts")}
	patterns := []string{"./...", "./..."}
	if directory, pattern, scoped := resolveScope(root, packagePattern); scoped {
		directories = []string{directory}
		patterns = []string{pattern}
	}
	for i, directory := range directories {
		shared.Stepf("Running go vet (%s)...", patterns[i])
		cmd := exec.Command("go", "vet", patterns[i])
		cmd.Dir = directory
		// The scripts module stays hermetic (never in go.work): vet it
		// with the workspace off. The root module vets inside it.
		env := shared.EnvWithoutGowork()
		if directory != root {
			env = shared.EnvWith([]string{"GOWORK=off"})
		}
		cmd.Env = env
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			shared.Failf("go vet: %v", err)
		}
	}
	shared.OKf("go vet passed")
}

// resolveScope maps a root-relative PKG pattern to the owning module:
// patterns under scripts/ run inside the scripts module with the path
// rebased, everything else runs in the root module. Empty and ./...
// patterns are unscoped (both modules). A pattern matching nothing fails
// loudly in go vet itself (typo protection).
func resolveScope(root, packagePattern string) (directory, pattern string, scoped bool) {
	clean := strings.TrimSpace(strings.ReplaceAll(packagePattern, "\\", "/"))
	clean = strings.TrimPrefix(clean, "./")
	if clean == "" || clean == "..." {
		return "", "", false
	}
	if clean == "scripts" || strings.HasPrefix(clean, "scripts/") {
		remainder := strings.TrimPrefix(strings.TrimPrefix(clean, "scripts"), "/")
		if remainder == "" {
			remainder = "..."
		}
		return filepath.Join(root, "scripts"), "./" + remainder, true
	}
	return root, "./" + clean, true
}
