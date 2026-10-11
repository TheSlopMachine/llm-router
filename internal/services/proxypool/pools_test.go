package proxypool

import (
	"context"
	"testing"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"

	stdErrors "errors"
)

// TestBuiltinFreePoolSeedsOnNew pins the provider foreign-key target: every
// fresh service carries the built-in "auto" row, and custom listings stay
// free of it.
func TestBuiltinFreePoolSeedsOnNew(t *testing.T) {
	service, err := New(testutil.SetupTestSQLite(t))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()
	pools, err := service.ListCustomPools(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pools) != 0 {
		t.Fatalf("custom pools must exclude the built-in free pool: %+v", pools)
	}
	if _, err := service.findCustomPool(ctx, builtinFreePoolID); err != nil {
		t.Fatalf("built-in free pool must resolve: %v", err)
	}
	if err := service.DeleteCustomPool(ctx, builtinFreePoolID); !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Fatalf("built-in delete must refuse with invalid input, got %v", err)
	}
}

// TestCustomPoolRoundTrip pins custom pool CRUD: save, name-ordered list,
// find by name, overwrite, and delete with not-found on missing.
func TestCustomPoolRoundTrip(t *testing.T) {
	service, err := New(testutil.SetupTestSQLite(t))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()
	saved, err := service.SaveCustomPool(ctx, &models.CustomProxyPool{
		Name:    "Second",
		Entries: []models.ProxyEntry{{URL: "http://192.0.2.21:8080", Country: "de"}},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.ID != "second" {
		t.Fatalf("slug id: got %q", saved.ID)
	}
	if _, err := service.SaveCustomPool(ctx, &models.CustomProxyPool{
		Name:    "First",
		Entries: []models.ProxyEntry{{URL: "http://192.0.2.22:8080"}},
	}); err != nil {
		t.Fatalf("save first: %v", err)
	}
	pools, err := service.ListCustomPools(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pools) != 2 || pools[0].Name != "First" || pools[1].Name != "Second" {
		t.Fatalf("name order: %+v", pools)
	}
	found, err := service.findCustomPool(ctx, "second")
	if err != nil || len(found.Entries) != 1 || found.Entries[0].Country != "DE" {
		t.Fatalf("find: %+v %v", found, err)
	}
	if err := service.DeleteCustomPool(ctx, "second"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := service.DeleteCustomPool(ctx, "second"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("second delete must report not found, got %v", err)
	}
}
