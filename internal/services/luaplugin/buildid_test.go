package luaplugin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/repository"
)

// seedBuildRepository inserts the plugin store row an origin references so
// the repository foreign key stays valid. Every call uses a distinct index
// URL so the unique index on the repository rows stays satisfied.
func seedBuildRepository(t *testing.T, service *Service, repoID string) {
	t.Helper()
	repositoryStore := repository.NewGormRepository[record.PluginRepository](service.database, "id")
	repositoryRow := record.PluginRepository{
		ID:          repoID,
		Kind:        "index",
		IndexURL:    "https://example.com/index/" + strings.ReplaceAll(repoID, "/", "-") + ".json",
		SourceURL:   "https://example.com/" + strings.ReplaceAll(repoID, "/", "-"),
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
	want := "tester/Test-Plugin"
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

// TestInstallSourceOverride verifies source interchangeability: the same
// author and plugin name installed from a different source, or from a file
// upload, id-overrides the previous installation and keeps the identity.
func TestInstallSourceOverride(t *testing.T) {
	service := setupService(t)
	seedBuildRepository(t, service, "github/owner/repo-a")
	seedBuildRepository(t, service, "github/owner/repo-b")

	repoA := PluginOrigin{RepoID: "github/owner/repo-a", Path: "llm-router-plugins/whatever.lua"}
	repoB := PluginOrigin{RepoID: "github/owner/repo-b", Path: "llm-router-plugins/other.lua"}
	rec, err := service.InstallSQLite(context.Background(), []byte(testPluginSource), repoA)
	if err != nil {
		t.Fatalf("install from repo a: %v", err)
	}
	override, err := service.InstallSQLite(context.Background(), []byte(testPluginSource), repoB)
	if err != nil {
		t.Fatalf("install from repo b: %v", err)
	}
	if override.ID != rec.ID {
		t.Fatalf("override changed identity: got %q, want %q", override.ID, rec.ID)
	}
	if override.Origin != repoB {
		t.Fatalf("override origin: got %+v, want %+v", override.Origin, repoB)
	}
	if _, err := service.GetSQLite(context.Background(), rec.ID); err != nil {
		t.Fatalf("one row per identity, get changed row: %v", err)
	}
	if _, err := service.Lookup("test-type"); err != nil {
		t.Fatalf("type key lost after override: %v", err)
	}

	fileOrigin := PluginOrigin{Manual: true}
	fromFile, err := service.InstallSQLite(context.Background(), []byte(testPluginSource), fileOrigin)
	if err != nil {
		t.Fatalf("install from file: %v", err)
	}
	if fromFile.ID != rec.ID {
		t.Fatalf("file install changed identity: got %q, want %q", fromFile.ID, rec.ID)
	}
}

func TestBuildIDIgnoresOrigin(t *testing.T) {
	manifest, err := ParseManifest([]byte(testPluginSource))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	origins := []PluginOrigin{
		{Manual: true},
		{RepoID: "bundled", Path: "llm-router-plugins/test-plugin.lua"},
		{RepoID: "github/owner/repo", Path: "llm-router-plugins/test-plugin.lua"},
		{RepoID: "generic/https-example-com-plugins-index-json", Path: "llm-router-plugins/test-plugin.lua"},
	}
	want := "tester/Test-Plugin"
	for _, origin := range origins {
		got, err := BuildID(origin, manifest)
		if err != nil {
			t.Errorf("BuildID(%+v): unexpected error: %v", origin, err)
			continue
		}
		if got != want {
			t.Errorf("BuildID(%+v): got %q, want %q", origin, got, want)
		}
	}
}

func TestBuildIDSlashRejection(t *testing.T) {
	// ParseManifest rejects slashes so the identifier alphabet stays
	// unambiguous; BuildID keeps its own guard for defense in depth.
	for _, source := range []string{
		strings.Replace(testPluginSource, "Test Plugin", "Test/Plugin", 1),
		strings.Replace(testPluginSource, "@author tester", "@author some/one", 1),
	} {
		if _, err := ParseManifest([]byte(source)); err == nil {
			t.Errorf("manifest with '/' accepted: %s", source)
		}
	}
	for _, manifest := range []*Manifest{
		{Author: "some/one", Plugin: "Test Plugin"},
		{Author: "tester", Plugin: "Test/Plugin"},
	} {
		if _, err := BuildID(PluginOrigin{Manual: true}, manifest); err == nil {
			t.Errorf("BuildID accepted slash-bearing manifest")
		}
	}
}
