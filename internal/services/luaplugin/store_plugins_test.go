//go:build store

package luaplugin

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStorePluginsInstall loads every plugin shipped in the sibling plugin
// store checkout and asserts registration-level invariants. It runs only
// with -tags=store and a sibling llm-router-store checkout: store coverage
// is opt-in, never a silent green skip in the default suite.
func TestStorePluginsInstall(t *testing.T) {
	storeDir := filepath.Join("..", "..", "llm-router-store", "llm-router-plugins")
	entries, err := os.ReadDir(storeDir)
	if err != nil {
		t.Fatalf("plugin store not checked out: %v", err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".lua" {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			svc := setupService(t)
			source, err := os.ReadFile(filepath.Join(storeDir, entry.Name()))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if _, err := svc.Install(source, PluginOrigin{Manual: true}); err != nil {
				t.Fatalf("install: %v", err)
			}
		})
	}
}

// TestStoreGooglePluginHandlers pins the Google plugin's endpoint handlers:
// chat, speech and image generation must all be registered after install.
func TestStoreGooglePluginHandlers(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "llm-router-store", "llm-router-plugins", "google.lua"))
	if err != nil {
		t.Fatalf("plugin store not checked out: %v", err)
	}
	svc := setupService(t)
	if _, err := svc.Install(source, PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	for _, handler := range []string{"complete", "complete_stream", "speech", "generate_image", "embed", "get_model_infos"} {
		if !svc.HasHandler("google", handler) {
			t.Fatalf("google plugin must register %q", handler)
		}
	}
}
