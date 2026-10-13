package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// setHomeDirectory points HOME and USERPROFILE at directory for the test.
func setHomeDirectory(test *testing.T, directory string) {
	test.Helper()
	test.Setenv("HOME", directory)
	test.Setenv("USERPROFILE", directory)
}

func TestExpandPath(t *testing.T) {
	homeDirectory := t.TempDir()
	setHomeDirectory(t, homeDirectory)
	t.Setenv("LLM_ROUTER_EXPAND_TEST_VALUE", "hello")
	os.Unsetenv("LLM_ROUTER_EXPAND_TEST_MISSING_UNIQUE")

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"blank", "   ", ""},
		{"relative", "relative/db.sqlite3", filepath.Clean("relative/db.sqlite3")},
		{"dot", "a/./b", filepath.Clean("a/b")},
		{"tilde", "~", filepath.Clean(homeDirectory)},
		{"tildeSlash", "~/data/db.sqlite3", filepath.Join(homeDirectory, "data/db.sqlite3")},
		{"tildeBackslash", `~\data\db.sqlite3`, filepath.Join(homeDirectory, "data/db.sqlite3")},
		{"tildeDollarRemainder", "~/$LLM_ROUTER_EXPAND_TEST_VALUE/db.sqlite3", filepath.Join(homeDirectory, "hello/db.sqlite3")},
		{"tildePercentRemainder", "~/%LLM_ROUTER_EXPAND_TEST_VALUE%/db.sqlite3", filepath.Join(homeDirectory, "hello/db.sqlite3")},
		{"absoluteTildeStays", "/test/~", filepath.Clean("/test/~")},
		{"windowsAbsoluteTildeStays", `C:\test\~`, filepath.Clean(`C:\test\~`)},
		{"dollar", "$LLM_ROUTER_EXPAND_TEST_VALUE/world", filepath.Clean("hello/world")},
		{"braced", "${LLM_ROUTER_EXPAND_TEST_VALUE}/world", filepath.Clean("hello/world")},
		{"missingDollar", "$LLM_ROUTER_EXPAND_TEST_MISSING_UNIQUE/world", filepath.Clean("/world")},
		{"percent", "%LLM_ROUTER_EXPAND_TEST_VALUE%/world", filepath.Clean("hello/world")},
		{"missingPercent", "%LLM_ROUTER_EXPAND_TEST_MISSING_UNIQUE%/world", filepath.Clean("/world")},
		{"escapedPercent", "%%", "%"},
		{"lonePercent", "100%", filepath.Clean("100%")},
		{"invalidPercentStays", "%HOME DIR%/x", filepath.Clean("%HOME DIR%/x")},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			expanded, err := ExpandPath(testCase.input)
			if err != nil {
				t.Fatalf("ExpandPath(%q): %v", testCase.input, err)
			}
			if expanded != testCase.want {
				t.Fatalf("ExpandPath(%q) = %q, want %q", testCase.input, expanded, testCase.want)
			}
		})
	}
}

func TestExpandPathRejectsTildeUser(t *testing.T) {
	setHomeDirectory(t, t.TempDir())
	for _, input := range []string{"~other/db.sqlite3", "~foo"} {
		if _, err := ExpandPath(input); !errors.Is(err, ErrUnsupportedTilde) {
			t.Fatalf("ExpandPath(%q) error = %v, want ErrUnsupportedTilde", input, err)
		}
	}
}

func TestLoadExpandsTildeDatabasePaths(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	homeDirectory := t.TempDir()
	setHomeDirectory(t, homeDirectory)
	t.Setenv("DB_PATH", "~/primary.sqlite3")
	t.Setenv("CACHE_PATH", "~\\cache.sqlite3")
	t.Setenv("EVENTS_PATH", "$HOME/events.sqlite3")

	configuration, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := filepath.Join(homeDirectory, "primary.sqlite3"); configuration.DBPath != want {
		t.Errorf("database = %q, want %q", configuration.DBPath, want)
	}
	if want := filepath.Join(homeDirectory, "cache.sqlite3"); configuration.CachePath != want {
		t.Errorf("cache = %q, want %q", configuration.CachePath, want)
	}
	if want := filepath.Join(homeDirectory, "events.sqlite3"); configuration.EventsPath != want {
		t.Errorf("events = %q, want %q", configuration.EventsPath, want)
	}
}

func TestLoadRejectsTildeUserDatabasePath(t *testing.T) {
	clearProcessEnvironment(t)
	isolateDotenv(t)
	setHomeDirectory(t, t.TempDir())
	t.Setenv("DB_PATH", "~other/primary.sqlite3")

	if _, _, err := Load(); !errors.Is(err, ErrUnsupportedTilde) {
		t.Fatalf("Load error = %v, want ErrUnsupportedTilde", err)
	}
}
