package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

// Uploads every .lua plugin in the store dir to the running dev stack via
// install-file. Version matches skip without reinstalling. Expects a running
// stack with authorization off (make start NO_AUTH=1); with auth on,
// install-file requires a logged-in session and fails loudly.
func main() {
	web := shared.WebBaseURL("WEB_PORT", "38080")
	store := shared.Getenv("UPLOAD_STORE_DIR", shared.DefaultStoreDir())
	if err := shared.WaitReady(web); err != nil {
		shared.Failf("%v", err)
	}
	entries, err := os.ReadDir(store)
	if err != nil {
		shared.Failf("read store dir %s: %v", store, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lua") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".lua"))
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
		v := shared.ManifestVersion(source)
		if ran {
			installed++
			fmt.Printf("[OK] %s %s installed\n", pluginType, v)
		} else {
			current++
			fmt.Printf("[>] %s %s already current\n", pluginType, v)
		}
	}
	shared.OKf("upload-plugins: %d installed, %d already current", installed, current)
}
