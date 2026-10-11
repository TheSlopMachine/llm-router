package repository

import (
	"context"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormRepository provides typed SQLite operations on the generics API.
// The handle stays at the base *gorm.DB. Each call derives its chain from
// it, so reused chains never leak conditions across calls.
type GormRepository[T any] struct {
	database   *gorm.DB
	keyColumns []clause.Column
}

// NewGormRepository creates a typed repository on the given handle.
// keyColumns names the primary key columns for upsert conflict targets, so
// secondary unique violations surface as duplicates instead of overwrites.
func NewGormRepository[T any](database *gorm.DB, keyColumns ...string) *GormRepository[T] {
	columns := make([]clause.Column, 0, len(keyColumns))
	for _, name := range keyColumns {
		columns = append(columns, clause.Column{Name: name})
	}
	return &GormRepository[T]{database: database, keyColumns: columns}
}

// Get returns the first row matching the given conditions.
// A miss returns the domain not-found sentinel.
func (r *GormRepository[T]) Get(ctx context.Context, conditions ...clause.Expression) (T, error) {
	var empty T
	row, err := gorm.G[T](r.database, conditions...).First(ctx)
	if err != nil {
		return empty, db.Translate(err)
	}
	return row, nil
}

// List returns every row matching the given conditions.
func (r *GormRepository[T]) List(ctx context.Context, conditions ...clause.Expression) ([]T, error) {
	rows, err := gorm.G[T](r.database, conditions...).Find(ctx)
	if err != nil {
		return nil, db.Translate(err)
	}
	return rows, nil
}

// Count returns the number of rows matching the given conditions.
func (r *GormRepository[T]) Count(ctx context.Context, conditions ...clause.Expression) (int64, error) {
	count, err := gorm.G[T](r.database, conditions...).Count(ctx, "*")
	if err != nil {
		return 0, db.Translate(err)
	}
	return count, nil
}

// Upsert writes rows in batches. Primary key conflicts update every column;
// secondary unique violations return a duplicate error.
func (r *GormRepository[T]) Upsert(ctx context.Context, rows ...T) error {
	if len(rows) == 0 {
		return nil
	}
	if err := gorm.G[T](r.database, clause.OnConflict{Columns: r.keyColumns, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
		return db.Translate(err)
	}
	return nil
}

// Delete removes rows matching the given conditions and returns the count.
func (r *GormRepository[T]) Delete(ctx context.Context, conditions ...clause.Expression) (int64, error) {
	affected, err := gorm.G[T](r.database, conditions...).Delete(ctx)
	if err != nil {
		return 0, db.Translate(err)
	}
	return int64(affected), nil
}

// Clear removes every row. Maintenance and tests own this path.
// The generics API has no session scope, so the global delete uses the
// traditional handle with AllowGlobalUpdate. This is the one listed use.
func (r *GormRepository[T]) Clear(ctx context.Context) error {
	var empty T
	result := r.database.WithContext(ctx).Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&empty)
	if result.Error != nil {
		return db.Translate(result.Error)
	}
	return nil
}
