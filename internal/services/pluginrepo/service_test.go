package pluginrepo

import (
	"errors"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func setupService(t *testing.T) *Service {
	t.Helper()
	return New(testutil.SetupTestDB(t))
}

func builtinID() string {
	return repoIDForIndex(BuiltinRepos[0].Index)
}

func TestEnsureBuiltinReposSeeds(t *testing.T) {
	svc := setupService(t)
	if err := svc.EnsureBuiltinRepos(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec, err := svc.Get(builtinID())
	if err != nil {
		t.Fatalf("get %q: %v", builtinID(), err)
	}
	if !rec.Builtin {
		t.Fatalf("repo %q not marked built-in", builtinID())
	}
	if rec.Kind != "index" || rec.IndexURL != BuiltinRepos[0].Index || rec.SourceURL != BuiltinRepos[0].Source {
		t.Fatalf("record mismatch: %+v", rec)
	}
}

func TestEnsureBuiltinReposIdempotent(t *testing.T) {
	svc := setupService(t)
	if err := svc.EnsureBuiltinRepos(); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	before, err := svc.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if err := svc.EnsureBuiltinRepos(); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	after, err := svc.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(before) != len(after) {
		t.Fatalf("record count changed: %d -> %d", len(before), len(after))
	}
}

func TestEnsureBuiltinReposMarksExisting(t *testing.T) {
	svc := setupService(t)
	rec := &RepoRecord{ID: builtinID(), Kind: "index", IndexURL: BuiltinRepos[0].Index}
	if err := svc.repo.Put(rec.ID, rec); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := svc.EnsureBuiltinRepos(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := svc.Get(builtinID())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Builtin || got.SourceURL != BuiltinRepos[0].Source {
		t.Fatalf("existing repo not adopted: %+v", got)
	}
}

func TestPruneLegacyRepos(t *testing.T) {
	svc := setupService(t)
	legacy := &RepoRecord{ID: "github/someone/elsewhere", Kind: "github"}
	if err := svc.repo.Put(legacy.ID, legacy); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := svc.EnsureBuiltinRepos(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := svc.PruneLegacyRepos(); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := svc.Get(legacy.ID); err == nil {
		t.Fatalf("legacy repo still present")
	}
	if _, err := svc.Get(builtinID()); err != nil {
		t.Fatalf("builtin repo pruned: %v", err)
	}
}

func TestRemoveBuiltinProtected(t *testing.T) {
	svc := setupService(t)
	if err := svc.EnsureBuiltinRepos(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	err := svc.Remove(builtinID())
	if !errors.Is(err, ErrBuiltinRepoProtected) {
		t.Fatalf("remove builtin: got %v, want ErrBuiltinRepoProtected", err)
	}
	if _, err := svc.Get(builtinID()); err != nil {
		t.Fatalf("builtin repo deleted: %v", err)
	}
}

func TestRemoveRegular(t *testing.T) {
	svc := setupService(t)
	rec := &RepoRecord{ID: "index/some-abc123", Kind: "index", IndexURL: "https://example.com/files/index.json"}
	if err := svc.repo.Put(rec.ID, rec); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := svc.Remove(rec.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := svc.Get(rec.ID); err == nil {
		t.Fatalf("repo still present after remove")
	}
}
