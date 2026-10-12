package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

// Uploads every .lua plugin in the store dir to the running dev stack via
// install-file. Version matches skip without reinstalling. Targets the
// admin listener (no login needed).
func main() {
	web := shared.AdminBaseURL()
	store := shared.Getenv("UPLOAD_STORE_DIR", shared.DefaultStoreDir())
	if err := shared.WaitReady(web); err != nil {
		shared.Failf("%v", err)
	}
	entries, err := os.ReadDir(store)
	if err != nil {
		shared.Failf("read store dir %s: %v", store, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".lua") {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name(), ".lua"))
	}
	sort.Strings(names)
	if len(names) == 0 {
		shared.Failf("no .lua plugins in %s", store)
	}
	installed, current := 0, 0
	for _, pluginType := range names {
		source, err := shared.PluginSource(store, pluginType)
		if err != nil {
			shared.Failf("%v", err)
		}
		ran, err := shared.InstallPlugin(web, pluginType, source)
		if err != nil {
			shared.Failf("%v", err)
		}
		version := shared.ManifestVersion(source)
		if ran {
			installed++
			fmt.Printf("[OK] %s %s installed\n", pluginType, version)
		} else {
			current++
			fmt.Printf("[>] %s %s already current\n", pluginType, version)
		}
	}
	shared.OKf("upload-plugins: %d installed, %d already current", installed, current)
}
