package db_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"gorm.io/gorm/clause"
)

func openConstraintDatabase(t *testing.T) *db.Database {
	t.Helper()
	path := filepath.Join(t.TempDir(), "constraints.sqlite3")
	database, err := db.OpenSQLite(context.Background(), path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	return database
}

func TestMissMapsToNotFound(t *testing.T) {
	ctx := context.Background()
	database := openConstraintDatabase(t)
	states := repository.NewGormRepository[record.ProxyState](database.Handle, "url")
	_, err := states.Get(ctx, clause.Eq{Column: "url", Value: "http://missing:8080"})
	if !errors.Is(err, apierrors.ErrNotFound) {
		t.Errorf("miss returns %v, want not found", err)
	}
}

func TestDuplicateTokenHashMapsToAlreadyExists(t *testing.T) {
	ctx := context.Background()
	database := openConstraintDatabase(t)
	tokens := repository.NewGormRepository[record.RouterToken](database.Handle, "id")
	now := time.Now().UTC()
	first := record.RouterToken{ID: "token-one", Name: "one", TokenHash: "hash-one", CreatedAt: now}
	if err := tokens.Upsert(ctx, first); err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	// Same primary key updates; the same hash on a new row must conflict.
	second := record.RouterToken{ID: "token-two", Name: "two", TokenHash: "hash-one", CreatedAt: now}
	if err := tokens.Upsert(ctx, second); !errors.Is(db.Translate(err), apierrors.ErrAlreadyExists) {
		t.Errorf("duplicate hash returns %v, want already exists", err)
	}
}

func TestVirtualNameKeyUnique(t *testing.T) {
	ctx := context.Background()
	database := openConstraintDatabase(t)
	models := repository.NewGormRepository[record.VirtualModel](database.Handle, "id")
	now := time.Now().UTC()
	first := record.VirtualModel{ID: "virtual-one", Name: "Chat", NameKey: "chat", CreatedAt: now, UpdatedAt: now}
	if err := models.Upsert(ctx, first); err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	second := record.VirtualModel{ID: "virtual-two", Name: "CHAT", NameKey: "chat", CreatedAt: now, UpdatedAt: now}
	if err := models.Upsert(ctx, second); !errors.Is(db.Translate(err), apierrors.ErrAlreadyExists) {
		t.Errorf("duplicate name key returns %v, want already exists", err)
	}
}

func TestRepositoryIndexURLUnique(t *testing.T) {
	ctx := context.Background()
	database := openConstraintDatabase(t)
	repositories := repository.NewGormRepository[record.PluginRepository](database.Handle, "id")
	now := time.Now().UTC()
	first := record.PluginRepository{ID: "repo-one", Kind: "index", IndexURL: "https://example.com/index.json", AddedAt: now}
	if err := repositories.Upsert(ctx, first); err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	second := record.PluginRepository{ID: "repo-two", Kind: "index", IndexURL: "https://example.com/index.json", AddedAt: now}
	if err := repositories.Upsert(ctx, second); !errors.Is(db.Translate(err), apierrors.ErrAlreadyExists) {
		t.Errorf("duplicate index url returns %v, want already exists", err)
	}
}

func TestInstanceSettingsIDCheck(t *testing.T) {
	ctx := context.Background()
	database := openConstraintDatabase(t)
	settings := repository.NewGormRepository[record.InstanceSetting](database.Handle, "id")
	if err := settings.Upsert(ctx, record.InstanceSetting{ID: 2, ModelsFilter: "all", UpdatedAt: time.Now().UTC()}); !errors.Is(db.Translate(err), apierrors.ErrInvalidInput) {
		t.Errorf("settings id 2 returns %v, want invalid input", err)
	}
	if err := settings.Upsert(ctx, record.InstanceSetting{ID: 1, ModelsFilter: "bogus", UpdatedAt: time.Now().UTC()}); !errors.Is(db.Translate(err), apierrors.ErrInvalidInput) {
		t.Errorf("settings filter bogus returns %v, want invalid input", err)
	}
}

func TestMetricDimensionCheck(t *testing.T) {
	ctx := context.Background()
	database := openConstraintDatabase(t)
	buckets := repository.NewGormRepository[record.MetricBucket](database.Handle, "granularity", "timestamp")
	providers := repository.NewGormRepository[record.MetricBucketProvider](database.Handle, "granularity", "timestamp", "dimension", "key")
	now := time.Now().UTC().Truncate(time.Second)
	if err := buckets.Upsert(ctx, record.MetricBucket{Granularity: "1m", Timestamp: now}); err != nil {
		t.Fatalf("upsert bucket: %v", err)
	}
	row := record.MetricBucketProvider{Granularity: "1m", Timestamp: now, Dimension: "bogus", Key: "provider"}
	if err := providers.Upsert(ctx, row); !errors.Is(db.Translate(err), apierrors.ErrInvalidInput) {
		t.Errorf("dimension bogus returns %v, want invalid input", err)
	}
}

func TestCompositeChildWithoutParentFails(t *testing.T) {
	ctx := context.Background()
	database := openConstraintDatabase(t)
	capabilities := repository.NewGormRepository[record.ModelOverrideCapability](database.Handle, "provider_id", "model_name", "capability")
	row := record.ModelOverrideCapability{ProviderID: "missing", ModelName: "model", Capability: "tools"}
	if err := capabilities.Upsert(ctx, row); !errors.Is(db.Translate(err), apierrors.ErrInvalidInput) {
		t.Errorf("orphan capability returns %v, want invalid input", err)
	}
}
