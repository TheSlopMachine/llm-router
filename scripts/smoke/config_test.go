package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePlugin(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestRegisterKey(t *testing.T) {
	cases := map[string]string{
		`llm_router.register("groq", {`:                  "groq",
		`  llm_router.register('kiro', {`:                "kiro",
		"llm_router.register(\"opencode-zen\", {})":      "opencode-zen",
		"-- llm_router.register(\"comment\", {}":         "",
		"print(\"no register here\")":                    "",
		"llm_router.register(":                           "",
		"llm_router.register(unquoted, {":                "",
		"llm_router.register(\"unterminated, {":          "",
		"llm_router.register(\"spaced\" , {":             "spaced",
		"llm_router.register(  \"padded\"  , {":          "padded",
		"local x = 1\nllm_router.register(\"second\", {": "second",
	}
	for input, want := range cases {
		if got := registerKey([]byte(input)); got != want {
			t.Errorf("registerKey(%q): got %q, want %q", input, got, want)
		}
	}
}

func TestDiscoverPlugins(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "groq.lua", "--- @plugin Groq\nllm_router.register(\"groq\", {})\n")
	writePlugin(t, dir, "renamed.lua", "--- @plugin Kiro\nllm_router.register(\"kiro\", {})\n")
	writePlugin(t, dir, "fallback.lua", "-- no register call\n")
	writePlugin(t, dir, "notes.txt", "llm_router.register(\"ignored\", {})\n")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writePlugin(t, dir, "sub/nested.lua", "llm_router.register(\"nested\", {})\n")

	sources, err := discoverPlugins(dir)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	want := map[string]string{
		"groq":     filepath.Join(dir, "groq.lua"),
		"kiro":     filepath.Join(dir, "renamed.lua"),
		"fallback": filepath.Join(dir, "fallback.lua"),
	}
	if len(sources) != len(want) {
		t.Fatalf("sources: got %v, want %v", sources, want)
	}
	for key, path := range want {
		if sources[key] != path {
			t.Errorf("key %q: got %q, want %q", key, sources[key], path)
		}
	}
}

func TestDiscoverPluginsDuplicate(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "one.lua", "llm_router.register(\"dup\", {})\n")
	writePlugin(t, dir, "two.lua", "llm_router.register(\"dup\", {})\n")
	if _, err := discoverPlugins(dir); err == nil {
		t.Fatal("duplicate keys must fail")
	} else if !strings.Contains(err.Error(), `"dup"`) {
		t.Fatalf("error must name the key: %v", err)
	}
}

func TestDiscoverPluginsEmpty(t *testing.T) {
	if _, err := discoverPlugins(t.TempDir()); err == nil {
		t.Fatal("empty store must fail")
	}
	if _, err := discoverPlugins(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing store must fail")
	}
}

func TestLoadConfigDiscoversPlugins(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "b-plugin.lua", "llm_router.register(\"b-type\", {})\n")
	writePlugin(t, dir, "a-plugin.lua", "llm_router.register(\"a-type\", {})\n")
	t.Setenv("SMOKE_STORE_DIR", dir)

	t.Setenv("SMOKE_PLUGINS", "all")
	all, err := loadConfig()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all.plugins) != 2 || all.plugins[0] != "a-type" || all.plugins[1] != "b-type" {
		t.Fatalf("all must expand sorted: %v", all.plugins)
	}
	if len(all.sources) != 2 {
		t.Fatalf("sources: %v", all.sources)
	}

	t.Setenv("SMOKE_PLUGINS", "B-TYPE,b-type")
	explicit, err := loadConfig()
	if err != nil {
		t.Fatalf("explicit: %v", err)
	}
	if len(explicit.plugins) != 1 || explicit.plugins[0] != "b-type" {
		t.Fatalf("explicit must normalize and dedupe: %v", explicit.plugins)
	}

	t.Setenv("SMOKE_PLUGINS", "nope")
	if _, err := loadConfig(); err == nil {
		t.Fatal("unknown plugin must fail")
	} else if !strings.Contains(err.Error(), `"nope"`) || !strings.Contains(err.Error(), "a-type") {
		t.Fatalf("error must name the entry and the available set: %v", err)
	}

	t.Setenv("SMOKE_STORE_DIR", filepath.Join(dir, "missing"))
	if _, err := loadConfig(); err == nil {
		t.Fatal("missing store must fail")
	}
}
