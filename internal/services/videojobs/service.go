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

// Service owns VideoJob records in the video_jobs bucket alongside the
// video_jobs table. The bbolt bucket serves live traffic; the SQLite table
// grows alongside it during the GORM migration and takes over at cutover.
// ProviderID stays a soft history reference on both backends: no foreign
// key, no join, no preload.
type Service struct {
	store    *repository.Repository[models.VideoJob]
	sqlite   *gorm.DB
	sqlStore *repository.GormRepository[record.VideoJob]
}

// New constructs a videojobs Service. Construction performs no I/O.
func New(database *db.DB) *Service {
	return &Service{
		store: repository.New[models.VideoJob](database, db.BucketVideoJobs, "video job"),
	}
}

// NewSQLite constructs a videojobs Service on the SQLite handle.
// Construction performs no I/O.
func NewSQLite(sqliteDatabase *db.Database) *Service {
	return &Service{
		sqlite:   sqliteDatabase.Handle,
		sqlStore: repository.NewGormRepository[record.VideoJob](sqliteDatabase.Handle, "id"),
	}
}

// WithSQLite attaches the alongside SQLite path to a bbolt service and
// returns the same Service. The bbolt bucket keeps serving live traffic
// until cutover.
func (s *Service) WithSQLite(sqliteDatabase *db.Database) *Service {
	s.sqlite = sqliteDatabase.Handle
	s.sqlStore = repository.NewGormRepository[record.VideoJob](sqliteDatabase.Handle, "id")
	return s
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

// sqliteWired fails closed when the SQLite path is not attached.
func (s *Service) sqliteWired() error {
	if s.sqlStore == nil || s.sqlite == nil {
		return errors.New("videojobs: sqlite store not wired")
	}
	return nil
}

// Create persists a new job row: one public method, one database.Update.
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
	return s.store.Put(job.ID, job)
}

// Get returns the job row by router-local ID.
func (s *Service) Get(id string) (*models.VideoJob, error) {
	job, err := s.store.Get(id)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, apierrors.NewNotFoundError("video job", id)
		}
		return nil, err
	}
	return job, nil
}

// SetStatus records a new upstream status: one public method, one database.Update.
func (s *Service) SetStatus(id, status string) error {
	return s.store.Update(id, func(job *models.VideoJob) error {
		job.Status = status
		job.UpdatedAt = time.Now()
		return nil
	})
}

// CreateSQLite persists a new job row: one public method, one transaction.
func (s *Service) CreateSQLite(ctx context.Context, job *models.VideoJob) error {
	if err := s.sqliteWired(); err != nil {
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

// GetSQLite returns the job row by router-local ID.
func (s *Service) GetSQLite(ctx context.Context, id string) (*models.VideoJob, error) {
	if err := s.sqliteWired(); err != nil {
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

// SetStatusSQLite records a new upstream status: one public method, one
// transaction holding the read-modify-write.
func (s *Service) SetStatusSQLite(ctx context.Context, id, status string) error {
	if err := s.sqliteWired(); err != nil {
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
