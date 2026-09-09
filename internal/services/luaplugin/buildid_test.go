package luaplugin

import (
	"testing"
)

func TestInstallRepoOrigin(t *testing.T) {
	svc := setupService(t)
	origin := PluginOrigin{RepoID: "github/TheSlopMachine/llm-router-store", Path: "llm-router-plugins/test-plugin.lua"}
	rec, err := svc.Install([]byte(testPluginSource), origin)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	want := "github/TheSlopMachine/llm-router-store/tester/test-plugin"
	if rec.ID != want {
		t.Fatalf("id: got %q, want %q", rec.ID, want)
	}
	if rec.Origin != origin {
		t.Fatalf("origin: got %+v, want %+v", rec.Origin, origin)
	}
	if _, err := svc.Lookup("test-type"); err != nil {
		t.Fatalf("lookup: %v", err)
	}
}

func TestInstallRepoOriginPreservesDisabled(t *testing.T) {
	svc := setupService(t)
	origin := PluginOrigin{RepoID: "github/owner/repo", Path: "llm-router-plugins/test-plugin.lua"}
	rec, err := svc.Install([]byte(testPluginSource), origin)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := svc.Disable(rec.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	updated, err := svc.Install([]byte(testPluginSource), origin)
	if err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if updated.ID != rec.ID {
		t.Fatalf("id changed: got %q, want %q", updated.ID, rec.ID)
	}
	if updated.Enabled {
		t.Fatalf("reinstall must preserve disabled state")
	}
	if len(updated.History) != 1 {
		t.Fatalf("history length: got %d, want 1", len(updated.History))
	}
}

func TestBuildIDRepoIDs(t *testing.T) {
	manifest, err := ParseManifest([]byte(testPluginSource))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	valid := []string{
		"bundled",
		"github/owner/repo",
		"github/TheSlopMachine/llm-router-store",
		"generic/https-example-com-plugins-index-json",
	}
	for _, id := range valid {
		origin := PluginOrigin{RepoID: id, Path: "llm-router-plugins/test-plugin.lua"}
		got, err := BuildID(origin, manifest)
		if err != nil {
			t.Errorf("BuildID(%q): unexpected error: %v", id, err)
			continue
		}
		want := id + "/tester/test-plugin"
		if got != want {
			t.Errorf("BuildID(%q): got %q, want %q", id, got, want)
		}
	}
	invalid := []string{
		"",
		"   ",
		"a/../b",
		"..",
		"/leading",
		"trailing/",
		"double//slash",
		"dot/./segment",
		"nul\x00byte",
	}
	for _, id := range invalid {
		origin := PluginOrigin{RepoID: id, Path: "llm-router-plugins/test-plugin.lua"}
		if _, err := BuildID(origin, manifest); err == nil {
			t.Errorf("BuildID(%q): expected error", id)
		}
	}
}
