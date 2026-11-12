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
	pidFile := shared.GetenvPath("PID_FILE", shared.DefaultPidFile())

	if parameters, err := shared.ReadStartParams(pidFile); err == nil {
		overlay("DB_PATH", parameters.DatabasePath, shared.DefaultDevDatabase())
		overlay("CACHE_PATH", parameters.CachePath, shared.DefaultDevCache())
		overlay("EVENTS_PATH", parameters.EventsPath, shared.DefaultDevEvents())
		overlay("ENV_FILE", parameters.EnvFile, shared.DefaultDevDotenv())
		overlay("HOST", parameters.Host, "localhost")
		overlay("PORT", parameters.Port, "38080")
		overlay("ADMIN_PORT", parameters.AdminPort, shared.DevAdminPort)
		overlay("LOG_LEVEL", parameters.LogLevel, "info")
		overlay("EVENTS_RETENTION_DAYS", parameters.EventsRetentionDays, "3")
		overlay("LUA_POOL_BUCKET_SIZE", parameters.LuaPoolBucketSize, "16")
	}

	// Always the whole stack: a kept vite serves the dependencies it loaded
	// at its own start (a bumped FUI stays old until vite restarts).
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
