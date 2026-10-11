package pluginrepo

import (
	"errors"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func setupService(t *testing.T) *Service {
	t.Helper()
	return New(testutil.SetupTestSQLite(t))
}

func builtinID() string {
	return repositoryIDForIndex(BuiltinRepos[0].Index)
}

func TestEnsureBuiltinReposSeeds(t *testing.T) {
	service := setupService(t)
	if err := service.EnsureBuiltinRepos(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	record, err := service.Get(builtinID())
	if err != nil {
		t.Fatalf("get %q: %v", builtinID(), err)
	}
	if !record.Builtin {
		t.Fatalf("repo %q not marked built-in", builtinID())
	}
	if record.Kind != "index" || record.IndexURL != BuiltinRepos[0].Index || record.SourceURL != BuiltinRepos[0].Source {
		t.Fatalf("record mismatch: %+v", record)
	}
}

// TestBuiltinRepoIDStable locks the historical record ID: repo IDs persist
// in storage and plugin origins, so the slug scheme must never drift.
// A scheme change orphans existing rows and duplicates catalog entries.
func TestBuiltinRepoIDStable(t *testing.T) {
	const want = "index/https-raw-githubusercontent-com-theslopmachine-llm-router-store--80dc9c77"
	if got := builtinID(); got != want {
		t.Fatalf("builtin repo ID drifted: got %q, want %q", got, want)
	}
}

func TestEnsureBuiltinReposIdempotent(t *testing.T) {
	service := setupService(t)
	if err := service.EnsureBuiltinRepos(); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	before, err := service.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if err := service.EnsureBuiltinRepos(); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	after, err := service.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(before) != len(after) {
		t.Fatalf("record count changed: %d -> %d", len(before), len(after))
	}
}

func TestEnsureBuiltinReposMarksExisting(t *testing.T) {
	service := setupService(t)
	record := &RepoRecord{ID: builtinID(), Kind: "index", IndexURL: BuiltinRepos[0].Index}
	if err := service.putRecord(record); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := service.EnsureBuiltinRepos(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := service.Get(builtinID())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Builtin || got.SourceURL != BuiltinRepos[0].Source {
		t.Fatalf("existing repo not adopted: %+v", got)
	}
}

func TestRemoveBuiltinProtected(t *testing.T) {
	service := setupService(t)
	if err := service.EnsureBuiltinRepos(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	err := service.Remove(builtinID())
	if !errors.Is(err, ErrBuiltinRepoProtected) {
		t.Fatalf("remove builtin: got %v, want ErrBuiltinRepoProtected", err)
	}
	if _, err := service.Get(builtinID()); err != nil {
		t.Fatalf("builtin repo deleted: %v", err)
	}
}

func TestRemoveRegular(t *testing.T) {
	service := setupService(t)
	record := &RepoRecord{ID: "index/some-abc123", Kind: "index", IndexURL: "https://example.com/files/index.json"}
	if err := service.putRecord(record); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := service.Remove(record.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := service.Get(record.ID); err == nil {
		t.Fatalf("repo still present after remove")
	}
}
