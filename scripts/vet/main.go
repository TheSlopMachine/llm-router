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
	pkg := shared.Getenv("PKG", "./...")
	// Unscoped runs cover both Go modules; a scoped PKG narrows to exactly
	// the requested tree.
	dirs := []string{root, filepath.Join(root, "scripts")}
	patterns := []string{"./...", "./..."}
	if dir, pattern, scoped := resolveScope(root, pkg); scoped {
		dirs = []string{dir}
		patterns = []string{pattern}
	}
	for i, dir := range dirs {
		shared.Stepf("Running go vet (%s)...", patterns[i])
		cmd := exec.Command("go", "vet", patterns[i])
		cmd.Dir = dir
		cmd.Env = shared.EnvWithoutGowork()
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
func resolveScope(root, pkg string) (dir, pattern string, scoped bool) {
	clean := strings.TrimSpace(strings.ReplaceAll(pkg, "\\", "/"))
	clean = strings.TrimPrefix(clean, "./")
	if clean == "" || clean == "..." {
		return "", "", false
	}
	if clean == "scripts" || strings.HasPrefix(clean, "scripts/") {
		rest := strings.TrimPrefix(strings.TrimPrefix(clean, "scripts"), "/")
		if rest == "" {
			rest = "..."
		}
		return filepath.Join(root, "scripts"), "./" + rest, true
	}
	return root, "./" + clean, true
}
