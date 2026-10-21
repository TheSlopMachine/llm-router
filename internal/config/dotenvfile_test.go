package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDotenv(t *testing.T, content string) DotenvFile {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return DotenvFile{Path: path}
}

func readDotenv(t *testing.T, file DotenvFile) string {
	t.Helper()
	raw, err := os.ReadFile(file.Path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(raw)
}

func TestDotenvSetKeepsCommentsAndOrder(t *testing.T) {
	file := writeDotenv(t, "# header\nHOST=localhost\n\n# port\nPORT=8080\nLOG_LEVEL=info\n")
	if err := file.Set("PORT", "9090"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	want := "# header\nHOST=localhost\n\n# port\nPORT=9090\nLOG_LEVEL=info\n"
	if got := readDotenv(t, file); got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
	info, err := os.Stat(file.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, %v, want 0600 preserved", info.Mode().Perm(), err)
	}
}

func TestDotenvSetAppendsMissingKey(t *testing.T) {
	file := writeDotenv(t, "HOST=localhost\n")
	if err := file.Set("PORT", "9090"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, want := readDotenv(t, file), "HOST=localhost\nPORT=9090\n"; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestDotenvSetCreatesFileFromTemplate(t *testing.T) {
	file := DotenvFile{Path: filepath.Join(t.TempDir(), ".env")}
	if err := file.Set("PORT", "9090"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got := readDotenv(t, file)
	if !strings.Contains(got, "PORT=9090\n") || !strings.Contains(got, "LOG_LEVEL=info\n") || strings.Contains(got, "PORT=8080") {
		t.Errorf("content = %q, want template with PORT=9090", got)
	}
}

func TestDotenvQuotesValuesWithSpaces(t *testing.T) {
	file := writeDotenv(t, "")
	if err := file.Set("DB_PATH", "/data/my dir/x.sqlite3"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := readDotenv(t, file); got != "DB_PATH='/data/my dir/x.sqlite3'\n" {
		t.Errorf("content = %q", got)
	}
	value, found, err := file.Lookup("DB_PATH")
	if err != nil || !found || value != "/data/my dir/x.sqlite3" {
		t.Errorf("Lookup = %q, %v, %v", value, found, err)
	}
	if err := file.Set("DB_PATH", "it's"); err == nil {
		t.Error("value with a quote must be rejected")
	}
}

func TestDotenvUnset(t *testing.T) {
	file := writeDotenv(t, "# c\nPORT=1\nHOST=h\nPORT=2\n")
	removed, err := file.Unset("PORT")
	if err != nil || !removed {
		t.Fatalf("Unset = %v, %v", removed, err)
	}
	if got, want := readDotenv(t, file), "# c\nHOST=h\n"; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
	removed, err = file.Unset("PORT")
	if err != nil || removed {
		t.Errorf("second Unset = %v, %v, want false, nil", removed, err)
	}
	missing := DotenvFile{Path: filepath.Join(t.TempDir(), "absent")}
	if removed, err := missing.Unset("PORT"); err != nil || removed {
		t.Errorf("Unset on missing file = %v, %v", removed, err)
	}
}

func TestDotenvLookupParsesForms(t *testing.T) {
	file := writeDotenv(t, "A=plain # comment\nexport B=\"quoted \\\"x\\\"\"\nC='single # kept'\nD=\n#E=hidden\n")
	cases := map[string]struct {
		value string
		found bool
	}{
		"A": {"plain", true},
		"B": {"quoted \"x\"", true},
		"C": {"single # kept", true},
		"D": {"", true},
		"E": {"", false},
	}
	for name, want := range cases {
		value, found, err := file.Lookup(name)
		if err != nil || found != want.found || value != want.value {
			t.Errorf("Lookup(%s) = %q, %v, %v, want %q, %v", name, value, found, err, want.value, want.found)
		}
	}
}

func TestResolveSettingsSources(t *testing.T) {
	for _, key := range Keys() {
		t.Setenv(key.Name, "")
	}
	file := writeDotenv(t, "PORT=9090\nHOST=\nLOG_LEVEL=debug\n")
	t.Setenv("LOG_LEVEL", "error")
	settings, err := ResolveSettings(file.Path)
	if err != nil {
		t.Fatalf("ResolveSettings: %v", err)
	}
	got := map[string]Setting{}
	for _, setting := range settings {
		got[setting.Key.Name] = setting
	}
	checks := []struct {
		name   string
		value  string
		source Source
	}{
		{"PORT", "9090", SourceFile},
		{"HOST", DefaultHost, SourceDefault},
		{"LOG_LEVEL", "error", SourceEnvironment},
		{"EVENTS_RETENTION_DAYS", "3", SourceDefault},
	}
	for _, check := range checks {
		if got[check.name].Value != check.value || got[check.name].Source != check.source {
			t.Errorf("%s = %q (%s), want %q (%s)", check.name, got[check.name].Value, got[check.name].Source, check.value, check.source)
		}
	}
}

func TestReplaceDefaultFileOverwrites(t *testing.T) {
	file := writeDotenv(t, "PORT=1\n")
	if err := ReplaceDefaultFile(file.Path); err != nil {
		t.Fatalf("ReplaceDefaultFile: %v", err)
	}
	if got := readDotenv(t, file); got != DefaultTemplate() {
		t.Errorf("content = %q, want the default template", got)
	}
}

func TestResolveSettingsPathDefaultsHonorXDG(t *testing.T) {
	for _, key := range Keys() {
		t.Setenv(key.Name, "")
	}
	t.Setenv("XDG_DATA_HOME", "/custom/data")
	settings, err := ResolveSettings(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range settings {
		if setting.Key.Name == "DB_PATH" && !strings.HasPrefix(filepath.ToSlash(setting.Value), "/custom/data/llm-router/") {
			t.Errorf("DB_PATH default = %q, want it under XDG_DATA_HOME", setting.Value)
		}
	}
}
