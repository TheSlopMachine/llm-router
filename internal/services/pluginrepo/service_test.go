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

func TestEnsureBuiltinReposSeeds(t *testing.T) {
	svc := setupService(t)
	if err := svc.EnsureBuiltinRepos(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, ref := range BuiltinRepos {
		rec, err := svc.Get(ref.ID)
		if err != nil {
			t.Fatalf("get %q: %v", ref.ID, err)
		}
		if !rec.Builtin {
			t.Fatalf("repo %q not marked built-in", ref.ID)
		}
		if rec.Kind != ref.Kind || rec.Owner != ref.Owner || rec.Repo != ref.Repo {
			t.Fatalf("repo %q fields mismatch: %+v", ref.ID, rec)
		}
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
	ref := BuiltinRepos[0]
	before, err := svc.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, rec := range before {
		if rec.ID == ref.ID {
			t.Fatalf("builtin repo %q already present", ref.ID)
		}
	}
	if err := svc.EnsureBuiltinRepos(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec, err := svc.Get(ref.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !rec.Builtin {
		t.Fatalf("existing repo not marked built-in")
	}
}

func TestRemoveBuiltinProtected(t *testing.T) {
	svc := setupService(t)
	if err := svc.EnsureBuiltinRepos(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	err := svc.Remove(BuiltinRepos[0].ID)
	if !errors.Is(err, ErrBuiltinRepoProtected) {
		t.Fatalf("remove builtin: got %v, want ErrBuiltinRepoProtected", err)
	}
	if _, err := svc.Get(BuiltinRepos[0].ID); err != nil {
		t.Fatalf("builtin repo deleted: %v", err)
	}
}

func TestRemoveRegular(t *testing.T) {
	svc := setupService(t)
	rec := &RepoRecord{ID: "github/someone/elsewhere", Kind: "github", Owner: "someone", Repo: "elsewhere"}
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
