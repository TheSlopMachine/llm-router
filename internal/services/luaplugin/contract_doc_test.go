package luaplugin

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// docs/PLUGIN-API.md is the complete plugin reference. These tests fail when
// the sandbox gains a function, a handler slot appears, or the quickstart
// plugin stops installing, without the document following.

func readPluginContract(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "PLUGIN-API.md"))
	if err != nil {
		t.Fatalf("read docs/PLUGIN-API.md: %v", err)
	}
	return string(content)
}

// sandboxAPINames lists every function and table the sandbox exposes under
// llm_router and json, as dotted paths ("credentials.park", "json.decode").
func sandboxAPINames(t *testing.T) []string {
	t.Helper()
	state := newSandboxState(&executionContext{capabilities: capabilities{credentials: true, proxies: true}, docker: &dockerBridge{}})
	defer state.Close()
	var names []string
	var walk func(prefix string, table *lua.LTable)
	walk = func(prefix string, table *lua.LTable) {
		table.ForEach(func(key, value lua.LValue) {
			name := key.String()
			if prefix != "" {
				name = prefix + "." + name
			}
			if nested, ok := value.(*lua.LTable); ok {
				walk(name, nested)
				return
			}
			names = append(names, name)
		})
	}
	router, ok := state.GetGlobal("llm_router").(*lua.LTable)
	if !ok {
		t.Fatal("sandbox has no llm_router table")
	}
	walk("", router)
	jsonTable, ok := state.GetGlobal("json").(*lua.LTable)
	if !ok {
		t.Fatal("sandbox has no json table")
	}
	walk("json", jsonTable)
	sort.Strings(names)
	return names
}

func TestPluginContractDocumentsEverySandboxFunction(t *testing.T) {
	contract := readPluginContract(t)
	names := sandboxAPINames(t)
	if len(names) == 0 {
		t.Fatal("sandbox exposes no functions")
	}
	for _, name := range names {
		documented := strings.Contains(contract, "llm_router."+name) ||
			strings.Contains(contract, "`"+name+"`") ||
			strings.Contains(contract, "`"+name+"(") ||
			strings.HasPrefix(name, "json.") && strings.Contains(contract, name+"(")
		if !documented {
			t.Errorf("docs/PLUGIN-API.md does not document %q", name)
		}
	}
}

func TestPluginContractDocumentsEveryHandlerSlot(t *testing.T) {
	contract := readPluginContract(t)
	for _, name := range append([]string{string(HandlerComplete)}, OptionalHandlerNames()...) {
		if !strings.Contains(contract, "`"+name+"`") {
			t.Errorf("docs/PLUGIN-API.md does not document handler %q", name)
		}
	}
}

func TestPluginContractQuickstartInstalls(t *testing.T) {
	contract := readPluginContract(t)
	section := contract[strings.Index(contract, "\n## Quickstart\n"):]
	start := strings.Index(section, "```lua\n")
	if start < 0 {
		t.Fatal("quickstart has no lua block")
	}
	source := section[start+len("```lua\n"):]
	source = source[:strings.Index(source, "\n```")]

	if !strings.Contains(source, "--- @plugin_api "+PluginAPIVersion.String()) {
		t.Errorf("quickstart does not declare @plugin_api %s", PluginAPIVersion.String())
	}
	service := setupService(t)
	if _, err := service.InstallSQLite(context.Background(), []byte(source), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("quickstart plugin does not install: %v", err)
	}
}
