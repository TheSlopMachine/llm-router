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
// output) are never touched.
func main() {
	root, err := shared.RootDir()
	if err != nil {
		shared.Failf("%v", err)
	}

	files, err := goFiles(root)
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
		shared.Failf("gofmt: %d files need formatting (run `make fmt`)", len(strings.Split(listed, "\n")))
	}
	shared.OKf("gofmt clean")
}

var skipDirs = map[string]bool{
	".git":         true,
	".workspace":   true,
	"node_modules": true,
	"build":        true,
}

// goFiles collects .go files under root, skipping third-party trees.
func goFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
	return files, err
}
