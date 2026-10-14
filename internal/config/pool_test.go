package config

import (
	"errors"
	"strings"
	"testing"
)

func TestLoadDefaultsPoolBucketSize(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	setHomeDirectory(t, t.TempDir())

	configuration, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if configuration.LuaPoolBucketSize != DefaultLuaPoolBucketSize {
		t.Fatalf("pool bucket size = %d, want %d", configuration.LuaPoolBucketSize, DefaultLuaPoolBucketSize)
	}
}

func TestLoadCustomPoolBucketSize(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	t.Setenv("LUA_POOL_BUCKET_SIZE", "4")

	configuration, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if configuration.LuaPoolBucketSize != 4 {
		t.Fatalf("pool bucket size = %d, want 4", configuration.LuaPoolBucketSize)
	}
}

func TestLoadZeroPoolBucketSizeDisables(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	t.Setenv("LUA_POOL_BUCKET_SIZE", "0")

	configuration, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if configuration.LuaPoolBucketSize != 0 {
		t.Fatalf("pool bucket size = %d, want 0", configuration.LuaPoolBucketSize)
	}
}

func TestLoadRejectsInvalidPoolBucketSize(t *testing.T) {
	for _, raw := range []string{"-1", "bogus", "1.5"} {
		clearProcessEnvironment(t)
		isolateDotenv(t)
		t.Setenv("LUA_POOL_BUCKET_SIZE", raw)

		if _, _, err := Load(); !errors.Is(err, ErrInvalidLuaPoolBucketSize) {
			t.Fatalf("LUA_POOL_BUCKET_SIZE=%q error = %v, want ErrInvalidLuaPoolBucketSize", raw, err)
		}
	}
}

func TestDefaultTemplateContainsPoolBucketSize(t *testing.T) {
	if !strings.Contains(DefaultTemplate(), "LUA_POOL_BUCKET_SIZE=16") {
		t.Fatal("template misses LUA_POOL_BUCKET_SIZE=16")
	}
}
