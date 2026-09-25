// restart stops the dev server and starts it again with the parameters of the
// last start (recorded in the params file next to the pidfile). Explicitly
// passed env overrides win over the recorded ones: a value equal to the
// built-in Makefile default is treated as "not passed".
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

func main() {
	pidFile := shared.Getenv("PID_FILE", shared.DefaultPidFile())

	if p, err := shared.ReadStartParams(pidFile); err == nil {
		overlay("DEV_DB", p.DevDB, shared.DefaultDevDB())
		overlay("HOST", p.Host, "localhost")
		overlay("WEB_PORT", p.WebPort, "38080")
		overlay("API_PORT", p.APIPort, "38081")
		overlay("LOG_LEVEL", p.LogLevel, "info")
		if p.NoAuth && !shared.IsWriteMode("NO_AUTH") {
			_ = os.Setenv("NO_AUTH", "1")
		}
	}

	root, err := shared.RootDir()
	if err != nil {
		shared.Failf("%v", err)
	}
	scriptsDir := filepath.Join(root, "scripts")

	run(scriptsDir, "stop")
	run(scriptsDir, "start")
}

// overlay applies the recorded value when the env var is unset or still holds
// the built-in default.
func overlay(key, recorded, builtinDefault string) {
	if recorded == "" {
		return
	}
	cur := strings.TrimSpace(os.Getenv(key))
	if cur == "" || cur == builtinDefault {
		_ = os.Setenv(key, recorded)
	}
}

func run(dir, target string) {
	cmd := exec.Command("go", "run", "./"+target)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	// Env is inherited as-is: the overlays above and GOWORK=off from the
	// Makefile both apply to the children.
	if err := cmd.Run(); err != nil {
		shared.Failf("%s: %v", target, err)
	}
}
