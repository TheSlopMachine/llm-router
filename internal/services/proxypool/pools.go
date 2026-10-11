package proxypool

import (
	"context"
	"fmt"
	"strings"

	stdErrors "errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// orderProxyPoolsByName sorts custom pool rows by name.
func orderProxyPoolsByName() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "name"}}}}
}

// builtinFreePoolID is the seeded free-pool row: the provider
// proxy_pool_id foreign key references it when operators select the "auto"
// pool. It carries no entries; the library free pool serves them live.
const builtinFreePoolID = "auto"

// EnsureBuiltinFreePool inserts the built-in free-pool row when missing.
// Existing rows stay untouched: reruns never rewrite timestamps. Call it
// wherever provider rows with proxy references write without the proxypool
// service in the stack; server wiring seeds it through New.
func EnsureBuiltinFreePool(ctx context.Context, database *db.Database) error {
	if _, err := gorm.G[record.ProxyPool](database.Handle, clause.Eq{Column: "id", Value: builtinFreePoolID}).First(ctx); err == nil {
		return nil
	} else if !stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
		return db.Translate(err)
	}
	now := util.Now().UTC()
	row := record.ProxyPool{
		ID: builtinFreePoolID, Name: builtinFreePoolID, Builtin: true,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := gorm.G[record.ProxyPool](database.Handle).Create(ctx, &row); err != nil {
		return db.Translate(err)
	}
	return nil
}

// ListCustomPools returns every manually managed proxy pool by name order.
// Built-in rows (the free pool) stay out: they are not custom pools.
func (s *Service) ListCustomPools(ctx context.Context) ([]*models.CustomProxyPool, error) {
	database, err := s.requireHandle()
	if err != nil {
		return nil, err
	}
	rows, err := gorm.G[record.ProxyPool](database.Handle,
		clause.Eq{Column: "builtin", Value: false},
		orderProxyPoolsByName()).Find(ctx)
	if err != nil {
		return nil, db.Translate(err)
	}
	result := make([]*models.CustomProxyPool, 0, len(rows))
	for _, row := range rows {
		entries, err := gorm.G[record.ProxyPoolEntry](database.Handle,
			clause.Eq{Column: "pool_id", Value: row.ID},
			clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "position"}}}}).Find(ctx)
		if err != nil {
			return nil, db.Translate(err)
		}
		restored := recordsToCustomPool(row, entries)
		result = append(result, &restored)
	}
	return result, nil
}

// SaveCustomPool creates or replaces a custom pool. IDs are slugified from
// the name when empty; entries with empty URLs are dropped. One transaction
// holds the pool upsert, the entry wipe, and the entry inserts on the
// transaction handle only.
func (s *Service) SaveCustomPool(ctx context.Context, pool *models.CustomProxyPool) (*models.CustomProxyPool, error) {
	if pool == nil {
		return nil, fmt.Errorf("custom proxy pool is required")
	}
	name := strings.TrimSpace(pool.Name)
	if name == "" {
		return nil, fmt.Errorf("custom proxy pool name is required")
	}
	identifier := strings.TrimSpace(pool.ID)
	if identifier == "" {
		identifier = slugify(name)
	}
	entries := make([]models.ProxyEntry, 0, len(pool.Entries))
	for _, entry := range pool.Entries {
		entryURL := strings.TrimSpace(entry.URL)
		if entryURL == "" {
			continue
		}
		entries = append(entries, models.ProxyEntry{URL: entryURL, Country: strings.ToUpper(strings.TrimSpace(entry.Country))})
	}
	database, err := s.requireHandle()
	if err != nil {
		return nil, err
	}
	now := util.Now().UTC()
	saved := &models.CustomProxyPool{ID: identifier, Name: name, Entries: entries, CreatedAt: pool.CreatedAt, UpdatedAt: now}
	if saved.CreatedAt.IsZero() {
		if previous, err := gorm.G[record.ProxyPool](database.Handle, clause.Eq{Column: "id", Value: identifier}).First(ctx); err == nil {
			saved.CreatedAt = previous.CreatedAt
		} else if !stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
			return nil, db.Translate(err)
		} else {
			saved.CreatedAt = now
		}
	}
	stored, persistedEntries := customPoolToRecords(*saved)
	if previous, err := gorm.G[record.ProxyPool](database.Handle, clause.Eq{Column: "id", Value: identifier}).First(ctx); err == nil {
		// Built-in rows stay built-in: a save over the free-pool ID
		// replaces entries, never the managed flag.
		stored.Builtin = previous.Builtin
		stored.CreatedAt = previous.CreatedAt
	} else if !stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
		return nil, db.Translate(err)
	}
	if err := database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		rows := []record.ProxyPool{stored}
		if err := gorm.G[record.ProxyPool](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
			return db.Translate(err)
		}
		if _, err := gorm.G[record.ProxyPoolEntry](transaction, clause.Eq{Column: "pool_id", Value: identifier}).Delete(ctx); err != nil {
			return db.Translate(err)
		}
		if len(persistedEntries) > 0 {
			entriesCopy := append([]record.ProxyPoolEntry(nil), persistedEntries...)
			if err := gorm.G[record.ProxyPoolEntry](transaction).CreateInBatches(ctx, &entriesCopy, 500); err != nil {
				return db.Translate(err)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return saved, nil
}

// DeleteCustomPool removes a custom pool by ID. Entries cascade from the
// pool foreign key. Built-in rows refuse: the free pool is not custom. A
// zero-row delete reports the not-found sentinel.
func (s *Service) DeleteCustomPool(ctx context.Context, identifier string) error {
	if strings.TrimSpace(identifier) == "" {
		return fmt.Errorf("custom proxy pool id is required")
	}
	database, err := s.requireHandle()
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(identifier)
	return database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		existing, err := gorm.G[record.ProxyPool](transaction, clause.Eq{Column: "id", Value: trimmed}).First(ctx)
		if err != nil {
			translated := db.Translate(err)
			if stdErrors.Is(translated, apierrors.ErrNotFound) {
				return fmt.Errorf("proxy pool %q: %w", trimmed, apierrors.ErrNotFound)
			}
			return translated
		}
		if existing.Builtin {
			return fmt.Errorf("proxy pool %q is built-in: %w", trimmed, apierrors.ErrInvalidInput)
		}
		tracker := gorm.WithResult()
		affected, err := gorm.G[record.ProxyPool](transaction, clause.Eq{Column: "id", Value: trimmed}, tracker).Delete(ctx)
		if err != nil {
			return db.Translate(err)
		}
		if affected == 0 || tracker.RowsAffected == 0 {
			return fmt.Errorf("proxy pool %q: %w", trimmed, apierrors.ErrNotFound)
		}
		return nil
	})
}

func (s *Service) findCustomPool(ctx context.Context, poolReference string) (*models.CustomProxyPool, error) {
	database, err := s.requireHandle()
	if err != nil {
		return nil, err
	}
	if row, err := gorm.G[record.ProxyPool](database.Handle, clause.Eq{Column: "id", Value: poolReference}).First(ctx); err == nil {
		entries, err := gorm.G[record.ProxyPoolEntry](database.Handle,
			clause.Eq{Column: "pool_id", Value: row.ID},
			clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "position"}}}}).Find(ctx)
		if err != nil {
			return nil, db.Translate(err)
		}
		restored := recordsToCustomPool(row, entries)
		return &restored, nil
	} else if !stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
		return nil, db.Translate(err)
	}
	all, err := s.ListCustomPools(ctx)
	if err != nil {
		return nil, err
	}
	for _, customPool := range all {
		if customPool != nil && strings.EqualFold(customPool.Name, poolReference) {
			return customPool, nil
		}
	}
	return nil, fmt.Errorf("proxy pool %q not found", poolReference)
}

func slugify(name string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			builder.WriteRune('-')
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		slug = "pool"
	}
	return slug
}

// requireHandle fails closed when the SQLite handle is missing.
func (s *Service) requireHandle() (*db.Database, error) {
	if s == nil || s.database == nil || s.database.Handle == nil {
		return nil, fmt.Errorf("proxypool sqlite handle is not configured: %w", apierrors.ErrInvalidInput)
	}
	return s.database, nil
}
