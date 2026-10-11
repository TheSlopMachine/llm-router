package pluginrepo

import (
	"context"
	"sort"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"gorm.io/gorm/clause"
)

// NewWithSQLite constructs a Service that mirrors every repository write to
// the plugin_repositories table and serves reads from it. bbolt stays
// populated alongside so the Wave 10 cutover keeps a complete copy.
func NewWithSQLite(database *db.DB, sqliteDatabase *db.Database) *Service {
	service := New(database)
	if sqliteDatabase != nil && sqliteDatabase.Handle != nil {
		service.sqlite = repository.NewGormRepository[record.PluginRepository](sqliteDatabase.Handle, "id")
	}
	return service
}

// toRepositoryRow converts the bbolt record to the SQLite row. Fields match
// one to one; the table carries no derived values.
func toRepositoryRow(value *RepoRecord) record.PluginRepository {
	return record.PluginRepository{
		ID:          value.ID,
		Kind:        value.Kind,
		IndexURL:    value.IndexURL,
		SourceURL:   value.SourceURL,
		Title:       value.Title,
		Description: value.Description,
		Builtin:     value.Builtin,
		AddedAt:     value.AddedAt,
	}
}

// toRepoRecord converts the SQLite row back to the bbolt record shape.
func toRepoRecord(row record.PluginRepository) *RepoRecord {
	return &RepoRecord{
		ID:          row.ID,
		Kind:        row.Kind,
		IndexURL:    row.IndexURL,
		SourceURL:   row.SourceURL,
		Title:       row.Title,
		Description: row.Description,
		Builtin:     row.Builtin,
		AddedAt:     row.AddedAt,
	}
}

// orderRepositoriesByID sorts repository rows by primary key.
func orderRepositoriesByID() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}}}}
}

// getRecord returns one repository by ID from SQLite when attached,
// otherwise from bbolt. A miss returns the domain not-found sentinel.
func (s *Service) getRecord(id string) (*RepoRecord, error) {
	if s.sqlite == nil {
		return s.store.Get(id)
	}
	row, err := s.sqlite.Get(context.Background(), clause.Eq{Column: "id", Value: id})
	if err != nil {
		return nil, err
	}
	return toRepoRecord(row), nil
}

// listRecords returns all repositories sorted by ID from SQLite when
// attached, otherwise from bbolt.
func (s *Service) listRecords() ([]*RepoRecord, error) {
	if s.sqlite == nil {
		items, err := s.store.List()
		if err != nil {
			return nil, err
		}
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		return items, nil
	}
	rows, err := s.sqlite.List(context.Background(), orderRepositoriesByID())
	if err != nil {
		return nil, err
	}
	items := make([]*RepoRecord, 0, len(rows))
	for _, row := range rows {
		items = append(items, toRepoRecord(row))
	}
	return items, nil
}

// putRecord writes one repository to bbolt, then mirrors it to SQLite when
// attached. bbolt stays first so the authoritative copy never lags the
// mirror. A mirror failure surfaces, never passes silently.
func (s *Service) putRecord(value *RepoRecord) error {
	if err := s.store.Put(value.ID, value); err != nil {
		return err
	}
	if s.sqlite == nil {
		return nil
	}
	return s.sqlite.Upsert(context.Background(), toRepositoryRow(value))
}

// deleteRecord removes one repository from bbolt, then from SQLite when
// attached. bbolt gates existence, so a missing ID fails before SQLite runs.
func (s *Service) deleteRecord(id string) error {
	if err := s.store.Delete(id); err != nil {
		return err
	}
	if s.sqlite == nil {
		return nil
	}
	_, err := s.sqlite.Delete(context.Background(), clause.Eq{Column: "id", Value: id})
	return err
}
