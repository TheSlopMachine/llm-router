package config

import (
	"strings"
	"testing"
)

func TestKeysMatchDefaultTemplate(t *testing.T) {
	template := DefaultTemplate()
	for _, key := range Keys() {
		if !strings.Contains(template, key.Name+"="+key.Default+"\n") {
			t.Errorf("template misses %s=%s", key.Name, key.Default)
		}
	}
	if strings.Contains(template, "ADMIN_PORT") {
		t.Error("template contains ADMIN_PORT")
	}
}

func TestLookupKeyIgnoresCase(t *testing.T) {
	key, found := LookupKey(" log_level ")
	if !found || key.Name != "LOG_LEVEL" {
		t.Fatalf("LookupKey = %v, %v, want LOG_LEVEL", key.Name, found)
	}
	if _, found := LookupKey("ADMIN_PORT"); found {
		t.Error("ADMIN_PORT must not be a configuration key")
	}
}

func TestKeyNormalize(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{"PORT", " 9090 ", "9090", false},
		{"PORT", "0", "", true},
		{"PORT", "http", "", true},
		{"HOST", "0.0.0.0", "0.0.0.0", false},
		{"HOST", "a b", "", true},
		{"LOG_LEVEL", "WARNING", "warn", false},
		{"LOG_LEVEL", "loud", "", true},
		{"EVENTS_RETENTION_DAYS", "7", "7", false},
		{"EVENTS_RETENTION_DAYS", "0", "", true},
		{"LUA_POOL_BUCKET_SIZE", "0", "0", false},
		{"LUA_POOL_BUCKET_SIZE", "-1", "", true},
		{"DB_PATH", "~/data/router.sqlite3", "~/data/router.sqlite3", false},
		{"DB_PATH", "a'b", "", true},
		{"DB_PATH", "   ", "", true},
	}
	for _, testCase := range cases {
		key, found := LookupKey(testCase.name)
		if !found {
			t.Fatalf("missing key %s", testCase.name)
		}
		got, err := key.Normalize(testCase.value)
		if (err != nil) != testCase.wantErr {
			t.Errorf("%s=%q: error = %v, wantErr %v", testCase.name, testCase.value, err, testCase.wantErr)
			continue
		}
		if err == nil && got != testCase.want {
			t.Errorf("%s=%q: got %q, want %q", testCase.name, testCase.value, got, testCase.want)
		}
	}
}

func TestCheckDistinctPaths(t *testing.T) {
	if err := CheckDistinctPaths(map[string]string{"DB_PATH": "/a/one", "CACHE_PATH": "/a/two", "EVENTS_PATH": "/a/three"}); err != nil {
		t.Errorf("distinct paths: %v", err)
	}
	err := CheckDistinctPaths(map[string]string{"DB_PATH": "/a/one", "CACHE_PATH": "/a/./one"})
	if err == nil || !strings.Contains(err.Error(), "DB_PATH and CACHE_PATH") {
		t.Errorf("equal paths: error = %v", err)
	}
}
