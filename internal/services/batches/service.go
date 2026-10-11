// Package batches persists router-side Anthropic message batches.
//
// One POST /v1/messages/batches creates a row holding the entries; the
// router executes entries against the chat pipeline and appends JSONL-ready
// results. Modeled on the videojobs service: one public method, one
// database.Update.
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
	store *repository.Repository[models.BatchRecord]
}

// New constructs a batches Service. Construction performs no I/O.
func New(database *db.DB) *Service {
	return &Service{
		store: repository.New[models.BatchRecord](database, db.BucketMessageBatches, "message batch"),
	}
}

// Create persists a new batch row: one public method, one database.Update.
func (s *Service) Create(record *models.BatchRecord) error {
	if record == nil {
		return errors.New("message batch is required")
	}
	if record.ID == "" {
		return errors.New("message batch id is required")
	}
	now := time.Now()
	record.CreatedAt = now
	record.UpdatedAt = now
	return s.store.Put(record.ID, record)
}

// Get returns the batch by ID.
func (s *Service) Get(id string) (*models.BatchRecord, error) {
	record, err := s.store.Get(id)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, apierrors.NewNotFoundError("message batch", id)
		}
		return nil, err
	}
	return record, nil
}

// Update rewrites a batch row: one public method, one database.Update.
func (s *Service) Update(record *models.BatchRecord) error {
	if record == nil || record.ID == "" {
		return errors.New("message batch id is required")
	}
	return s.store.Update(record.ID, func(current *models.BatchRecord) error {
		record.CreatedAt = current.CreatedAt
		record.UpdatedAt = time.Now()
		*current = *record
		return nil
	})
}

// Delete removes a batch row. Missing rows are not errors.
func (s *Service) Delete(id string) error {
	return s.store.DeleteIfExists(id)
}

// List returns all batches, oldest first (ID tiebreaks clock ties).
func (s *Service) List() ([]*models.BatchRecord, error) {
	records, err := s.store.List()
	if err != nil {
		return nil, err
	}
	if records == nil {
		records = []*models.BatchRecord{}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].ID < records[j].ID
		}
		return records[i].CreatedAt.Before(records[j].CreatedAt)
	})
	return records, nil
}
