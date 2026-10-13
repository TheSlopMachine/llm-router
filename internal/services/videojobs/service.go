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
	"log/slog"
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
	logger   *slog.Logger
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

// SetLogger wires structured logging (called once from server.New).
func (s *Service) SetLogger(l *slog.Logger) { s.logger = l }

func (s *Service) log() *slog.Logger {
	if s == nil || s.logger == nil {
		return slog.Default()
	}
	return s.logger
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
		err := errors.New("videojobs: sqlite store not wired")
		s.log().Error("Video job store not wired", "error", err)
		return err
	}
	return nil
}

// Create persists a new job row: one public method, one transaction.
func (s *Service) Create(ctx context.Context, job *models.VideoJob) error {
	if err := s.requireStore(); err != nil {
		return err
	}
	if job == nil {
		s.log().Debug("Video job create validation failed", "reason", "job required")
		return errors.New("video job is required")
	}
	if job.ID == "" {
		s.log().Debug("Video job create validation failed", "reason", "job id required")
		return errors.New("video job id is required")
	}
	now := time.Now().UTC()
	job.CreatedAt = now
	job.UpdatedAt = now
	row := toRecord(job)
	replaced := false
	err := s.sqlite.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txStore := repository.NewGormRepository[record.VideoJob](tx, "id")
		if _, getErr := txStore.Get(ctx, clause.Eq{Column: "id", Value: job.ID}); getErr == nil {
			replaced = true
		}
		return txStore.Upsert(ctx, row)
	})
	if err != nil {
		s.log().Error("Video job create failed", "job_id", job.ID, "error", err)
		return err
	}
	if replaced {
		s.log().Info("Video job replaced", "job_id", job.ID, "status", job.Status, "model", string(job.Model))
	} else {
		s.log().Info("Video job created", "job_id", job.ID, "status", job.Status, "model", string(job.Model))
	}
	return nil
}

// Get returns the job row by router-local ID.
func (s *Service) Get(ctx context.Context, id string) (*models.VideoJob, error) {
	if err := s.requireStore(); err != nil {
		return nil, err
	}
	row, err := s.sqlStore.Get(ctx, clause.Eq{Column: "id", Value: id})
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.log().Debug("Video job not found", "job_id", id)
			return nil, apierrors.NewNotFoundError("video job", id)
		}
		s.log().Error("Video job fetch failed", "job_id", id, "error", err)
		return nil, err
	}
	job := fromRecord(row)
	s.log().Debug("Video job fetched", "job_id", id, "status", job.Status)
	return job, nil
}

// SetStatus records a new upstream status: one public method, one
// transaction holding the read-modify-write.
func (s *Service) SetStatus(ctx context.Context, id, status string) error {
	if err := s.requireStore(); err != nil {
		return err
	}
	err := s.sqlite.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txStore := repository.NewGormRepository[record.VideoJob](tx, "id")
		row, err := txStore.Get(ctx, clause.Eq{Column: "id", Value: id})
		if err != nil {
			if errors.Is(err, apierrors.ErrNotFound) {
				s.log().Debug("Video job not found", "job_id", id)
				return apierrors.NewNotFoundError("video job", id)
			}
			s.log().Error("Video job status update failed", "job_id", id, "status", status, "error", err)
			return err
		}
		row.Status = status
		row.UpdatedAt = time.Now().UTC()
		if err := txStore.Upsert(ctx, row); err != nil {
			s.log().Error("Video job status update failed", "job_id", id, "status", status, "error", err)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !models.ValidVideoStatus(status) {
		s.log().Warn("Video job status unknown", "job_id", id, "status", status)
	}
	s.log().Info("Video job status updated", "job_id", id, "status", status)
	return nil
}
