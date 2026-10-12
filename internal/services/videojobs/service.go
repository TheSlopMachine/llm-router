// Package videojobs persists router-side video generation jobs.
//
// Each POST /v1/videos creates one row mapping the router-local job ID
// handed to the client to the upstream job ID polled later. Poll and
// content requests resolve the row first, so they need no provider context
// of their own; the stored backend model and provider drive the upstream
// calls while the stored request model drives token authorization.
package videojobs

import (
	"context"
	"errors"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Service owns VideoJob records in the video_jobs table. ProviderID stays
// a soft history reference: no foreign key, no join, no preload.
type Service struct {
	sqlite   *gorm.DB
	sqlStore *repository.GormRepository[record.VideoJob]
}

// New constructs a videojobs Service on the cache SQLite handle.
// Construction performs no I/O.
func New(sqliteDatabase *db.Database) *Service {
	if sqliteDatabase == nil || sqliteDatabase.Cache == nil {
		return &Service{}
	}
	return &Service{
		sqlite:   sqliteDatabase.Cache,
		sqlStore: repository.NewGormRepository[record.VideoJob](sqliteDatabase.Cache, "id"),
	}
}

// toRecord maps the wire job to the storage row. Model IDs travel as
// strings; ProviderID copies verbatim as a soft reference.
func toRecord(job *models.VideoJob) record.VideoJob {
	return record.VideoJob{
		ID:            job.ID,
		Model:         string(job.Model),
		BackendModel:  string(job.BackendModel),
		ProviderID:    job.ProviderID,
		TypeKey:       job.TypeKey,
		UpstreamJobID: job.UpstreamJobID,
		Status:        job.Status,
		CreatedAt:     job.CreatedAt,
		UpdatedAt:     job.UpdatedAt,
	}
}

// fromRecord maps the storage row to the wire job.
func fromRecord(row record.VideoJob) *models.VideoJob {
	return &models.VideoJob{
		ID:            row.ID,
		Model:         models.ModelId(row.Model),
		BackendModel:  models.ModelId(row.BackendModel),
		ProviderID:    row.ProviderID,
		TypeKey:       row.TypeKey,
		UpstreamJobID: row.UpstreamJobID,
		Status:        row.Status,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

// requireStore fails closed when the SQLite path is not attached.
func (s *Service) requireStore() error {
	if s == nil || s.sqlStore == nil || s.sqlite == nil {
		return errors.New("videojobs: sqlite store not wired")
	}
	return nil
}

// Create persists a new job row: one public method, one transaction.
func (s *Service) Create(ctx context.Context, job *models.VideoJob) error {
	if err := s.requireStore(); err != nil {
		return err
	}
	if job == nil {
		return errors.New("video job is required")
	}
	if job.ID == "" {
		return errors.New("video job id is required")
	}
	now := time.Now().UTC()
	job.CreatedAt = now
	job.UpdatedAt = now
	row := toRecord(job)
	return s.sqlite.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txStore := repository.NewGormRepository[record.VideoJob](tx, "id")
		return txStore.Upsert(ctx, row)
	})
}

// Get returns the job row by router-local ID.
func (s *Service) Get(ctx context.Context, id string) (*models.VideoJob, error) {
	if err := s.requireStore(); err != nil {
		return nil, err
	}
	row, err := s.sqlStore.Get(ctx, clause.Eq{Column: "id", Value: id})
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, apierrors.NewNotFoundError("video job", id)
		}
		return nil, err
	}
	return fromRecord(row), nil
}

// SetStatus records a new upstream status: one public method, one
// transaction holding the read-modify-write.
func (s *Service) SetStatus(ctx context.Context, id, status string) error {
	if err := s.requireStore(); err != nil {
		return err
	}
	return s.sqlite.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txStore := repository.NewGormRepository[record.VideoJob](tx, "id")
		row, err := txStore.Get(ctx, clause.Eq{Column: "id", Value: id})
		if err != nil {
			if errors.Is(err, apierrors.ErrNotFound) {
				return apierrors.NewNotFoundError("video job", id)
			}
			return err
		}
		row.Status = status
		row.UpdatedAt = time.Now().UTC()
		return txStore.Upsert(ctx, row)
	})
}
