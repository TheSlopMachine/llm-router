package pluginrepo

import (
	"context"
	"errors"
	"testing"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func setupGormService(t *testing.T) *Service {
	t.Helper()
	return New(testutil.SetupTestSQLite(t))
}

func TestGormEnsureBuiltinReposSeedsAndMirrors(t *testing.T) {
	service := setupGormService(t)
	if err := service.EnsureBuiltinRepos(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	listed, err := service.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != builtinID() || !listed[0].Builtin {
		t.Fatalf("list: %+v", listed)
	}
	stored, err := service.Get(builtinID())
	if err != nil {
		t.Fatalf("stored row missing: %v", err)
	}
	if !stored.Builtin || stored.IndexURL != BuiltinRepos[0].Index {
		t.Fatalf("stored row mismatch: %+v", stored)
	}
}

func TestGormEnsureBuiltinReposIdempotent(t *testing.T) {
	service := setupGormService(t)
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

func TestGormEnsureBuiltinReposMarksExisting(t *testing.T) {
	service := setupGormService(t)
	seed := &RepoRecord{ID: builtinID(), Kind: "index", IndexURL: BuiltinRepos[0].Index}
	if err := service.putRecord(seed); err != nil {
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
		t.Fatalf("existing repository not adopted: %+v", got)
	}
}

func TestGormAddRepoDirectIndex(t *testing.T) {
	server := newIndexTestServer()
	defer server.Close()
	service := setupGormService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	raw := server.URL + "/files/index.json"
	stored, err := service.AddRepo(ctx, raw)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if stored.Kind != "index" || stored.IndexURL != raw || stored.Title != "Test store" {
		t.Fatalf("stored: %+v", stored)
	}
	again, err := service.AddRepo(ctx, raw)
	if err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if again.ID != stored.ID {
		t.Fatalf("re-add changed id: %q -> %q", stored.ID, again.ID)
	}
	mirror, err := service.Get(stored.ID)
	if err != nil {
		t.Fatalf("stored row missing: %v", err)
	}
	if mirror.Title != "Test store" {
		t.Fatalf("stored row mismatch: %+v", mirror)
	}
}

func TestGormListSorted(t *testing.T) {
	service := setupGormService(t)
	for _, item := range []*RepoRecord{
		{ID: "index/b-second", Kind: "index", IndexURL: "https://example.com/b/index.json"},
		{ID: "index/a-first", Kind: "index", IndexURL: "https://example.com/a/index.json"},
	} {
		if err := service.putRecord(item); err != nil {
			t.Fatalf("put %q: %v", item.ID, err)
		}
	}
	listed, err := service.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 || listed[0].ID != "index/a-first" || listed[1].ID != "index/b-second" {
		t.Fatalf("unsorted list: %+v", listed)
	}
}

func TestGormRemoveRegular(t *testing.T) {
	service := setupGormService(t)
	seed := &RepoRecord{ID: "index/some-abc123", Kind: "index", IndexURL: "https://example.com/files/index.json"}
	if err := service.putRecord(seed); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := service.Remove(seed.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := service.Get(seed.ID); !errors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("get after remove: got %v, want not-found", err)
	}
}

func TestGormRemoveBuiltinProtected(t *testing.T) {
	service := setupGormService(t)
	if err := service.EnsureBuiltinRepos(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := service.Remove(builtinID()); !errors.Is(err, ErrBuiltinRepoProtected) {
		t.Fatalf("remove builtin: got %v, want protected", err)
	}
	if _, err := service.Get(builtinID()); err != nil {
		t.Fatalf("builtin repository deleted: %v", err)
	}
}

func TestGormRemoveMissingReportsNotFound(t *testing.T) {
	service := setupGormService(t)
	if err := service.Remove("index/missing"); !errors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("remove missing: got %v, want not-found", err)
	}
}
