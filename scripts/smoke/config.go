package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

// Valid SMOKE_TARGETS tokens: text-only by default, heavy on request.
var validTargets = map[string]bool{
	"completions": true,
	"messages":    true,
	"transcribe":  true,
	"speech":      true,
	"image":       true,
	"embeddings":  true,
	"video":       true,
}

type configuration struct {
	web     string
	api     string
	plugins []string
	targets map[string]bool
	store   string
	cleanup bool
	// virtualModelName is this run's ad-hoc virtual model name: unique per run so a
	// leftover row from an earlier, interrupted run can never collide with
	// it. Keeps the "smoke-vm" prefix so cleanupStaleVirtualModels can find
	// and remove any such leftovers before this run starts.
	virtualModelName string
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// adminBaseURL builds the admin listener URL unless an explicit SMOKE_*
// override is set. The admin listener binds loopback regardless of HOST,
// so this never uses HOST.
func adminBaseURL(override string) string {
	if v := os.Getenv(override); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return fmt.Sprintf("http://127.0.0.1:%s", getenv("ADMIN_PORT", shared.DevAdminPort))
}

func loadConfig() (configuration, error) {
	targets := map[string]bool{}
	for _, target := range strings.Split(getenv("SMOKE_TARGETS", "completions,messages"), ",") {
		target = strings.ToLower(strings.TrimSpace(target))
		if target == "" {
			continue
		}
		if !validTargets[target] {
			return configuration{}, fmt.Errorf("unknown SMOKE_TARGETS entry %q (want completions,messages,transcribe,speech,image,embeddings,video)", target)
		}
		targets[target] = true
	}
	plugins := []string{}
	for _, plugin := range strings.Split(getenv("SMOKE_PLUGINS", "mock"), ",") {
		plugin = strings.ToLower(strings.TrimSpace(plugin))
		if plugin == "" {
			continue
		}
		if plugin == "all" {
			plugins = []string{"mock", "groq", "google", "kiro", "opencode-zen", "opencode-free"}
			break
		}
		plugins = append(plugins, plugin)
	}
	return configuration{
		// Everything runs against the admin listener: same JSON API
		// without authentication, loopback only.
		web:              adminBaseURL("SMOKE_WEB"),
		api:              adminBaseURL("SMOKE_API"),
		plugins:          plugins,
		targets:          targets,
		store:            getenv("SMOKE_STORE_DIR", "../../llm-router-store/llm-router-plugins"),
		cleanup:          getenv("SMOKE_CLEANUP", "1") == "1",
		virtualModelName: fmt.Sprintf("smoke-vm-%d", time.Now().UnixNano()),
	}, nil
}

// knownTypes lists runnable provider types. Unknown types fail fast: a
// typo in SMOKE_PLUGINS must be loud, never a silent skip.
var knownTypes = map[string]bool{
	"mock": true, "groq": true, "google": true,
	"kiro": true, "opencode-zen": true, "opencode-free": true,
}
