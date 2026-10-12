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

	if params, err := shared.ReadStartParams(pidFile); err == nil {
		overlay("DEV_DB", params.DevDB, shared.DefaultDevDB())
		overlay("DEV_CACHE", params.DevCache, shared.DefaultDevCache())
		overlay("DB_PATH", params.DevDB, "")
		overlay("CACHE_PATH", params.DevCache, "")
		overlay("HOST", params.Host, "localhost")
		overlay("PORT", params.Port, "38080")
		overlay("LOG_LEVEL", params.LogLevel, "info")
		if params.NoAuth && !shared.IsWriteMode("NO_AUTH") {
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

func run(directory, target string) {
	cmd := exec.Command("go", "run", "./"+target)
	cmd.Dir = directory
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	// Env is inherited as-is: the overlays above and GOWORK=off from the
	// Makefile both apply to the children.
	if err := cmd.Run(); err != nil {
		shared.Failf("%s: %v", target, err)
	}
}
