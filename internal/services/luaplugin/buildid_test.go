package luaplugin

import (
	"context"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/repository"
)

// seedBuildRepository inserts the plugin store row an origin references so
// the repository foreign key stays valid.
func seedBuildRepository(t *testing.T, service *Service, repoID string) {
	t.Helper()
	repositoryStore := repository.NewGormRepository[record.PluginRepository](service.database, "id")
	repositoryRow := record.PluginRepository{
		ID:          repoID,
		Kind:        "index",
		IndexURL:    "https://example.com/index.json",
		SourceURL:   "https://example.com",
		Title:       "Build",
		Description: "Build repository",
		AddedAt:     time.Now().UTC(),
	}
	if err := repositoryStore.Upsert(context.Background(), repositoryRow); err != nil {
		t.Fatalf("seed repository: %v", err)
	}
}

func TestInstallRepoOrigin(t *testing.T) {
	service := setupService(t)
	seedBuildRepository(t, service, "github/TheSlopMachine/llm-router-store")
	origin := PluginOrigin{RepoID: "github/TheSlopMachine/llm-router-store", Path: "llm-router-plugins/test-plugin.lua"}
	rec, err := service.InstallSQLite(context.Background(), []byte(testPluginSource), origin)
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
	if _, err := service.Lookup("test-type"); err != nil {
		t.Fatalf("lookup: %v", err)
	}
}

func TestInstallRepoOriginKeepsHistory(t *testing.T) {
	service := setupService(t)
	seedBuildRepository(t, service, "github/owner/repo")
	origin := PluginOrigin{RepoID: "github/owner/repo", Path: "llm-router-plugins/test-plugin.lua"}
	rec, err := service.InstallSQLite(context.Background(), []byte(testPluginSource), origin)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	updated, err := service.InstallSQLite(context.Background(), []byte(testPluginSource), origin)
	if err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if updated.ID != rec.ID {
		t.Fatalf("id changed: got %q, want %q", updated.ID, rec.ID)
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
