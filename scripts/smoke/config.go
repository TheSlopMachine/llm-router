package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	// sources maps discovered plugin type keys to source files.
	sources map[string]string
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
	store := getenv("SMOKE_STORE_DIR", "../../llm-router-store/llm-router-plugins")
	sources, err := discoverPlugins(store)
	if err != nil {
		return configuration{}, err
	}
	seen := map[string]bool{}
	for _, plugin := range strings.Split(getenv("SMOKE_PLUGINS", "all"), ",") {
		plugin = strings.ToLower(strings.TrimSpace(plugin))
		if plugin == "" {
			continue
		}
		if plugin == "all" {
			plugins = sortedKeys(sources)
			break
		}
		if _, ok := sources[plugin]; !ok {
			return configuration{}, fmt.Errorf("unknown SMOKE_PLUGINS entry %q (available: %s)", plugin, strings.Join(sortedKeys(sources), ", "))
		}
		if !seen[plugin] {
			seen[plugin] = true
			plugins = append(plugins, plugin)
		}
	}
	return configuration{
		// Everything runs against the admin listener: same JSON API
		// without authentication, loopback only.
		web:              adminBaseURL("SMOKE_WEB"),
		api:              adminBaseURL("SMOKE_API"),
		plugins:          plugins,
		targets:          targets,
		store:            store,
		sources:          sources,
		virtualModelName: fmt.Sprintf("smoke-vm-%d", time.Now().UnixNano()),
	}, nil
}

// discoverPlugins maps plugin type keys to source files under the store
// dir. Keys parse from llm_router.register("key"); files without a key
// fall back to the filename stem. A missing dir or zero Lua files is an
// error: unknown state surfaces, never a silent empty run.
func discoverPlugins(storeDir string) (map[string]string, error) {
	entries, err := os.ReadDir(storeDir)
	if err != nil {
		return nil, fmt.Errorf("plugin store %q: %w", storeDir, err)
	}
	sources := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".lua") {
			continue
		}
		path := filepath.Join(storeDir, entry.Name())
		source, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("plugin source %q: %w", path, err)
		}
		key := registerKey(source)
		if key == "" {
			key = strings.TrimSuffix(entry.Name(), ".lua")
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		if prev, dup := sources[key]; dup {
			return nil, fmt.Errorf("plugin type %q from %q and %q", key, prev, path)
		}
		sources[key] = path
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("plugin store %q holds no .lua files", storeDir)
	}
	return sources, nil
}

// registerKey reads the llm_router.register("key") type key from a plugin
// source. It returns "" when no key parses.
func registerKey(source []byte) string {
	for _, line := range strings.Split(string(source), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		rest, ok := strings.CutPrefix(trimmed, "llm_router.register(")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		if len(rest) < 2 || (rest[0] != '"' && rest[0] != '\'') {
			continue
		}
		if end := strings.IndexByte(rest[1:], rest[0]); end >= 0 {
			return rest[1 : 1+end]
		}
	}
	return ""
}

// sortedKeys returns the map keys in ascending order.
func sortedKeys(sources map[string]string) []string {
	keys := make([]string, 0, len(sources))
	for key := range sources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
