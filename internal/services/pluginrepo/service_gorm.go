package pluginrepo

import (
	"context"
	"fmt"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"gorm.io/gorm/clause"
)

// requireSQLite fails closed when the SQLite handle is missing.
func (s *Service) requireSQLite() (*repository.GormRepository[record.PluginRepository], error) {
	if s == nil || s.sqlite == nil {
		return nil, fmt.Errorf("pluginrepo sqlite handle is not configured")
	}
	return s.sqlite, nil
}

// toRepositoryRow converts the repository record to the SQLite row. Fields
// match one to one; the table carries no derived values.
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

// toRepoRecord converts the SQLite row back to the repository record shape.
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

// getRecord returns one repository by ID. A miss returns the domain
// not-found sentinel.
func (s *Service) getRecord(id string) (*RepoRecord, error) {
	sqlite, err := s.requireSQLite()
	if err != nil {
		return nil, err
	}
	row, err := sqlite.Get(context.Background(), clause.Eq{Column: "id", Value: id})
	if err != nil {
		return nil, err
	}
	return toRepoRecord(row), nil
}

// listRecords returns all repositories sorted by ID.
func (s *Service) listRecords() ([]*RepoRecord, error) {
	sqlite, err := s.requireSQLite()
	if err != nil {
		return nil, err
	}
	rows, err := sqlite.List(context.Background(), orderRepositoriesByID())
	if err != nil {
		return nil, err
	}
	items := make([]*RepoRecord, 0, len(rows))
	for _, row := range rows {
		items = append(items, toRepoRecord(row))
	}
	return items, nil
}

// putRecord writes one repository.
func (s *Service) putRecord(value *RepoRecord) error {
	sqlite, err := s.requireSQLite()
	if err != nil {
		return err
	}
	return sqlite.Upsert(context.Background(), toRepositoryRow(value))
}

// deleteRecord removes one repository. A missing ID fails through the
// not-found sentinel so callers never silently drop nothing.
func (s *Service) deleteRecord(id string) error {
	sqlite, err := s.requireSQLite()
	if err != nil {
		return err
	}
	if _, err := s.getRecord(id); err != nil {
		return err
	}
	_, err = sqlite.Delete(context.Background(), clause.Eq{Column: "id", Value: id})
	return err
}
