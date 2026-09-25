package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

// fmt checks (default) or writes (FMT_WRITE=1) gofmt formatting across both
// Go modules. Third-party trees (.workspace clones, node_modules, build
// output) are never touched. PATHS optionally narrows to space-separated
// root-relative files or directories (default: the whole tree).
func main() {
	root, err := shared.RootDir()
	if err != nil {
		shared.Failf("%v", err)
	}

	files, err := goFiles(scopeRoots(root))
	if err != nil {
		shared.Failf("%v", err)
	}
	if len(files) == 0 {
		shared.Failf("no Go files found under %s", root)
	}

	if shared.IsWriteMode("FMT_WRITE") {
		shared.Stepf("Running gofmt -w on %d files...", len(files))
		cmd := exec.Command("gofmt", append([]string{"-w"}, files...)...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			shared.Failf("gofmt -w: %v", err)
		}
		shared.OKf("gofmt applied")
		return
	}

	shared.Stepf("Checking gofmt on %d files...", len(files))
	cmd := exec.Command("gofmt", append([]string{"-l"}, files...)...)
	out, err := cmd.Output()
	if err != nil {
		shared.Failf("gofmt -l: %v", err)
	}
	listed := strings.TrimSpace(string(out))
	if listed != "" {
		fmt.Printf("%s\n", listed)
		shared.Failf("gofmt: %d files need formatting (run `make go-fmt`)", len(strings.Split(listed, "\n")))
	}
	shared.OKf("gofmt clean")
}

var skipDirs = map[string]bool{
	".git":         true,
	".workspace":   true,
	"node_modules": true,
	"build":        true,
}

// scopeRoots resolves PATHS to walk roots: each entry must exist under
// root (file or directory), otherwise the run fails loudly.
func scopeRoots(root string) []string {
	raw := strings.Fields(shared.Getenv("PATHS", ""))
	if len(raw) == 0 {
		return []string{root}
	}
	roots := make([]string, 0, len(raw))
	for _, p := range raw {
		clean := filepath.Clean(strings.ReplaceAll(p, "\\", "/"))
		full := filepath.Join(root, clean)
		if _, err := os.Stat(full); err != nil {
			shared.Failf("PATHS entry %q not found under %s", p, root)
		}
		roots = append(roots, full)
	}
	return roots
}

// goFiles collects .go files under the given roots, skipping third-party
// trees. Single files pass through directly.
func goFiles(roots []string) ([]string, error) {
	var files []string
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			if strings.HasSuffix(root, ".go") {
				files = append(files, root)
			}
			continue
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != root && skipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".go") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}
