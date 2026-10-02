// Package videojobs persists router-side video generation jobs.
//
// Each POST /v1/videos creates one row mapping the router-local job ID
// handed to the client to the upstream job ID polled later. Poll and
// content requests resolve the row first, so they need no provider context
// of their own; the stored backend model and provider drive the upstream
// calls while the stored request model drives token authorization.
package videojobs

import (
	"errors"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
)

// Service owns VideoJob records in the video_jobs bucket.
type Service struct {
	repo *repository.Repository[models.VideoJob]
}

// New constructs a videojobs Service. Construction performs no I/O.
func New(database *db.DB) *Service {
	return &Service{
		repo: repository.New[models.VideoJob](database, db.BucketVideoJobs, "video job"),
	}
}

// Create persists a new job row: one public method, one db.Update.
func (s *Service) Create(job *models.VideoJob) error {
	if job == nil {
		return errors.New("video job is required")
	}
	if job.ID == "" {
		return errors.New("video job id is required")
	}
	now := time.Now()
	job.CreatedAt = now
	job.UpdatedAt = now
	return s.repo.Put(job.ID, job)
}

// Get returns the job row by router-local ID.
func (s *Service) Get(id string) (*models.VideoJob, error) {
	job, err := s.repo.Get(id)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, apierrors.NewNotFoundError("video job", id)
		}
		return nil, err
	}
	return job, nil
}

// SetStatus records a new upstream status: one public method, one db.Update.
func (s *Service) SetStatus(id, status string) error {
	return s.repo.Update(id, func(job *models.VideoJob) error {
		job.Status = status
		job.UpdatedAt = time.Now()
		return nil
	})
}
