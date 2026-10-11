// Package responses persists router-side compatibility records.
//
// Responses, conversations, assistants, threads, thread messages and runs
// share one bucket (ResponseRecord envelope with a Kind discriminator),
// modeled on the videojobs service: one public method, one database.Update.
package responses

import (
	"errors"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
)

// Service owns ResponseRecord rows in the responses bucket.
type Service struct {
	store *repository.Repository[models.ResponseRecord]
}

// New constructs a responses Service. Construction performs no I/O.
func New(database *db.DB) *Service {
	return &Service{
		store: repository.New[models.ResponseRecord](database, db.BucketResponses, "compat record"),
	}
}

// Create persists a new record: one public method, one database.Update.
func (s *Service) Create(record *models.ResponseRecord) error {
	if record == nil {
		return errors.New("compat record is required")
	}
	if record.ID == "" {
		return errors.New("compat record id is required")
	}
	now := time.Now()
	record.CreatedAt = now
	record.UpdatedAt = now
	return s.store.Put(record.ID, record)
}

// Get returns the record by ID, optionally asserting its kind.
func (s *Service) Get(id string, kinds ...models.ResponseRecordKind) (*models.ResponseRecord, error) {
	record, err := s.store.Get(id)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, apierrors.NewNotFoundError("compat record", id)
		}
		return nil, err
	}
	for _, kind := range kinds {
		if record.Kind == kind {
			return record, nil
		}
	}
	if len(kinds) > 0 {
		return nil, apierrors.NewNotFoundError("compat record", id)
	}
	return record, nil
}

// Update rewrites a record in place: one public method, one database.Update.
func (s *Service) Update(record *models.ResponseRecord) error {
	if record == nil || record.ID == "" {
		return errors.New("compat record id is required")
	}
	return s.store.Update(record.ID, func(current *models.ResponseRecord) error {
		record.CreatedAt = current.CreatedAt
		record.UpdatedAt = time.Now()
		*current = *record
		return nil
	})
}

// Delete removes a record. Missing rows are not errors.
func (s *Service) Delete(id string) error {
	return s.store.DeleteIfExists(id)
}

// List returns records of the given kinds (empty kinds = all), oldest first.
// ParentID filters thread children and run bindings when non-empty.
func (s *Service) List(kinds []models.ResponseRecordKind, parentID string) ([]*models.ResponseRecord, error) {
	wantedKinds := map[models.ResponseRecordKind]bool{}
	for _, kind := range kinds {
		wantedKinds[kind] = true
	}
	records, err := s.store.ListFiltered(func(record *models.ResponseRecord) bool {
		if len(wantedKinds) > 0 && !wantedKinds[record.Kind] {
			return false
		}
		if parentID != "" && record.ParentID != parentID {
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if records == nil {
		records = []*models.ResponseRecord{}
	}
	// Oldest first: bucket order is key-lexicographic, and IDs are random,
	// so creation order needs an explicit sort (ID tiebreaks clock ties).
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].ID < records[j].ID
		}
		return records[i].CreatedAt.Before(records[j].CreatedAt)
	})
	return records, nil
}
