// Package responses persists router-side compatibility records.
//
// Responses, conversations, assistants, threads, thread messages and runs
// share one bucket (ResponseRecord envelope with a Kind discriminator),
// modeled on the videojobs service: one public method, one db.Update.
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
	repo *repository.Repository[models.ResponseRecord]
}

// New constructs a responses Service. Construction performs no I/O.
func New(database *db.DB) *Service {
	return &Service{
		repo: repository.New[models.ResponseRecord](database, db.BucketResponses, "compat record"),
	}
}

// Create persists a new record: one public method, one db.Update.
func (s *Service) Create(rec *models.ResponseRecord) error {
	if rec == nil {
		return errors.New("compat record is required")
	}
	if rec.ID == "" {
		return errors.New("compat record id is required")
	}
	now := time.Now()
	rec.CreatedAt = now
	rec.UpdatedAt = now
	return s.repo.Put(rec.ID, rec)
}

// Get returns the record by ID, optionally asserting its kind.
func (s *Service) Get(id string, kinds ...models.ResponseRecordKind) (*models.ResponseRecord, error) {
	rec, err := s.repo.Get(id)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, apierrors.NewNotFoundError("compat record", id)
		}
		return nil, err
	}
	for _, k := range kinds {
		if rec.Kind == k {
			return rec, nil
		}
	}
	if len(kinds) > 0 {
		return nil, apierrors.NewNotFoundError("compat record", id)
	}
	return rec, nil
}

// Update rewrites a record in place: one public method, one db.Update.
func (s *Service) Update(rec *models.ResponseRecord) error {
	if rec == nil || rec.ID == "" {
		return errors.New("compat record id is required")
	}
	return s.repo.Update(rec.ID, func(cur *models.ResponseRecord) error {
		rec.CreatedAt = cur.CreatedAt
		rec.UpdatedAt = time.Now()
		*cur = *rec
		return nil
	})
}

// Delete removes a record. Missing rows are not errors.
func (s *Service) Delete(id string) error {
	return s.repo.DeleteIfExists(id)
}

// List returns records of the given kinds (empty kinds = all), oldest first.
// ParentID filters thread children and run bindings when non-empty.
func (s *Service) List(kinds []models.ResponseRecordKind, parentID string) ([]*models.ResponseRecord, error) {
	want := map[models.ResponseRecordKind]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	recs, err := s.repo.ListFiltered(func(r *models.ResponseRecord) bool {
		if len(want) > 0 && !want[r.Kind] {
			return false
		}
		if parentID != "" && r.ParentID != parentID {
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if recs == nil {
		recs = []*models.ResponseRecord{}
	}
	// Oldest first: bucket order is key-lexicographic, and IDs are random,
	// so creation order needs an explicit sort (ID tiebreaks clock ties).
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].CreatedAt.Equal(recs[j].CreatedAt) {
			return recs[i].ID < recs[j].ID
		}
		return recs[i].CreatedAt.Before(recs[j].CreatedAt)
	})
	return recs, nil
}
