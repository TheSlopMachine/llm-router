// Package batches persists router-side Anthropic message batches.
//
// One POST /v1/messages/batches creates a row holding the entries; the
// router executes entries against the chat pipeline and appends JSONL-ready
// results. Modeled on the videojobs service: one public method, one
// db.Update.
package batches

import (
	"errors"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
)

// Terminal batch states: no further transitions leave them.
const (
	StatusInProgress = "in_progress"
	StatusEnded      = "ended"
	StatusCanceled   = "canceled"
)

// Service owns BatchRecord rows in the message_batches bucket.
type Service struct {
	repo *repository.Repository[models.BatchRecord]
}

// New constructs a batches Service. Construction performs no I/O.
func New(database *db.DB) *Service {
	return &Service{
		repo: repository.New[models.BatchRecord](database, db.BucketMessageBatches, "message batch"),
	}
}

// Create persists a new batch row: one public method, one db.Update.
func (s *Service) Create(rec *models.BatchRecord) error {
	if rec == nil {
		return errors.New("message batch is required")
	}
	if rec.ID == "" {
		return errors.New("message batch id is required")
	}
	now := time.Now()
	rec.CreatedAt = now
	rec.UpdatedAt = now
	return s.repo.Put(rec.ID, rec)
}

// Get returns the batch by ID.
func (s *Service) Get(id string) (*models.BatchRecord, error) {
	rec, err := s.repo.Get(id)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, apierrors.NewNotFoundError("message batch", id)
		}
		return nil, err
	}
	return rec, nil
}

// Update rewrites a batch row: one public method, one db.Update.
func (s *Service) Update(rec *models.BatchRecord) error {
	if rec == nil || rec.ID == "" {
		return errors.New("message batch id is required")
	}
	return s.repo.Update(rec.ID, func(cur *models.BatchRecord) error {
		rec.CreatedAt = cur.CreatedAt
		rec.UpdatedAt = time.Now()
		*cur = *rec
		return nil
	})
}

// Delete removes a batch row. Missing rows are not errors.
func (s *Service) Delete(id string) error {
	return s.repo.DeleteIfExists(id)
}

// List returns all batches, oldest first (ID tiebreaks clock ties).
func (s *Service) List() ([]*models.BatchRecord, error) {
	recs, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	if recs == nil {
		recs = []*models.BatchRecord{}
	}
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].CreatedAt.Equal(recs[j].CreatedAt) {
			return recs[i].ID < recs[j].ID
		}
		return recs[i].CreatedAt.Before(recs[j].CreatedAt)
	})
	return recs, nil
}
