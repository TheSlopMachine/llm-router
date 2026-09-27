package luaplugin

import (
	"testing"
)

// TestParseStorageKey_RoundTrip locks the storage key format at its owner:
// ParseStorageKey inverts storageKey exactly, including scopes and keys
// carrying ":" and "/" (the separators other subsystems once assumed).
func TestParseStorageKey_RoundTrip(t *testing.T) {
	cases := []struct{ plugin, scope, key string }{
		{"plug-1", "flow:abc", "device"},
		{"plug-1", "auth", "a/b/c"},
		{"plug-1", "scope:with:colons", "key:with:colons/and/slashes"},
	}
	for _, c := range cases {
		raw := storageKey(c.plugin, c.scope, c.key)
		plugin, scope, key, ok := ParseStorageKey(raw)
		if !ok {
			t.Fatalf("parse %q: not ok", raw)
		}
		if plugin != c.plugin || scope != c.scope || key != c.key {
			t.Fatalf("parse %q = %q/%q/%q, want %q/%q/%q",
				raw, plugin, scope, key, c.plugin, c.scope, c.key)
		}
	}
}

func TestParseStorageKey_RejectsMalformed(t *testing.T) {
	for _, raw := range []string{"", "no-separators", "only\x00two", "\x00scope\x00key"} {
		if _, _, _, ok := ParseStorageKey(raw); ok {
			t.Fatalf("parse %q: unexpectedly ok", raw)
		}
	}
	// Empty scope/key still parse: the parser only promises the plugin
	// namespace, never payload validity.
	if _, _, _, ok := ParseStorageKey("plugin\x00\x00"); !ok {
		t.Fatal("parse plugin with empty scope/key: not ok")
	}
}
