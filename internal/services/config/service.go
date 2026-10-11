// Package config manages RouterConfiguration persisted in the RouterConfiguration bucket.
//
// The bbolt path serves live traffic. The SQLite path grows alongside it
// during the GORM migration and takes over at cutover: the single
// instance_settings row carries the configuration columns plus the
// bootstrapped flag.
package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
)

const instanceKey = "instance"

// instanceRowID pins the single instance_settings row. The table CHECK
// enforces id = 1; the service never reads or writes another identifier.
const instanceRowID int64 = 1

// Service manages RouterConfiguration.
type Service struct {
	database *db.DB
	sqlite   *db.Database
	settings *repository.GormRepository[record.InstanceSetting]
}

// New constructs a config Service on the bbolt handle. The SQLite path
// stays disabled until NewWithSQLite supplies the SQLite handle.
func New(database *db.DB) *Service {
	return &Service{database: database}
}

// NewWithSQLite constructs a config Service with both handles. The bbolt
// path serves live traffic until Wave 10; the SQLite path is additive.
func NewWithSQLite(database *db.DB, sqlite *db.Database) *Service {
	service := New(database)
	if sqlite != nil {
		service.sqlite = sqlite
		service.settings = repository.NewGormRepository[record.InstanceSetting](sqlite.Handle, "id")
	}
	return service
}

// Get returns the stored RouterConfiguration.
func (s *Service) Get() (models.RouterConfiguration, error) {
	var routerConfiguration models.RouterConfiguration
	err := s.database.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(db.BucketRouterConfiguration)
		if bucket == nil {
			return nil
		}
		value := bucket.Get([]byte(instanceKey))
		if value == nil {
			return nil
		}
		return json.Unmarshal(value, &routerConfiguration)
	})
	if err != nil {
		return models.RouterConfiguration{}, err
	}
	return routerConfiguration, nil
}

// Put validates and persists RouterConfiguration.
func (s *Service) Put(routerConfiguration models.RouterConfiguration) error {
	if err := routerConfiguration.Validate(); err != nil {
		return err
	}
	encoded, err := json.Marshal(routerConfiguration)
	if err != nil {
		return fmt.Errorf("marshal RouterConfiguration: %w", err)
	}
	if err := s.database.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(db.BucketRouterConfiguration)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(instanceKey), encoded)
	}); err != nil {
		return err
	}
	return nil
}

// ─────────────────────────────────────────────
// SQLite (GORM) path
// ─────────────────────────────────────────────

// GetSettings returns the stored RouterConfiguration from instance_settings.
// A missing row returns the zero configuration, matching the bbolt path.
func (s *Service) GetSettings(ctx context.Context) (models.RouterConfiguration, error) {
	settings, err := s.requireSettings()
	if err != nil {
		return models.RouterConfiguration{}, err
	}
	row, err := settings.Get(ctx, clause.Eq{Column: "id", Value: instanceRowID})
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return models.RouterConfiguration{}, nil
		}
		return models.RouterConfiguration{}, fmt.Errorf("get instance settings: %w", err)
	}
	return toRouterConfiguration(row), nil
}

// PutSettings validates and persists RouterConfiguration to
// instance_settings. The bootstrapped flag survives the write: one
// transaction reads the row and upserts the merged record through the
// transaction handle only.
func (s *Service) PutSettings(ctx context.Context, configuration models.RouterConfiguration) error {
	if err := configuration.Validate(); err != nil {
		return fmt.Errorf("validate router configuration: %w", err)
	}
	sqlite, err := s.requireDatabase()
	if err != nil {
		return err
	}
	if err := sqlite.Handle.Transaction(func(transaction *gorm.DB) error {
		bootstrapped, err := loadBootstrapped(ctx, transaction)
		if err != nil {
			return err
		}
		return upsertInstanceRow(ctx, transaction, toInstanceRecord(configuration, bootstrapped, time.Now().UTC()))
	}); err != nil {
		return db.Translate(err)
	}
	return nil
}

// IsBootstrapped reports the instance_settings bootstrapped flag. A missing
// row reads false: unknown state never reports bootstrapped.
func (s *Service) IsBootstrapped(ctx context.Context) (bool, error) {
	settings, err := s.requireSettings()
	if err != nil {
		return false, err
	}
	row, err := settings.Get(ctx, clause.Eq{Column: "id", Value: instanceRowID})
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("get bootstrapped flag: %w", err)
	}
	return row.Bootstrapped, nil
}

// SetBootstrapped marks the instance bootstrapped and preserves the stored
// configuration columns. One transaction, transaction handle only.
func (s *Service) SetBootstrapped(ctx context.Context) error {
	sqlite, err := s.requireDatabase()
	if err != nil {
		return err
	}
	if err := sqlite.Handle.Transaction(func(transaction *gorm.DB) error {
		row, err := loadInstanceRow(ctx, transaction)
		if err != nil {
			return err
		}
		row.Bootstrapped = true
		row.UpdatedAt = time.Now().UTC()
		return upsertInstanceRow(ctx, transaction, row)
	}); err != nil {
		return db.Translate(err)
	}
	return nil
}

// ─────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────

func (s *Service) requireDatabase() (*db.Database, error) {
	if s == nil || s.sqlite == nil {
		return nil, fmt.Errorf("config sqlite handle is not configured: %w", apierrors.ErrInvalidInput)
	}
	return s.sqlite, nil
}

func (s *Service) requireSettings() (*repository.GormRepository[record.InstanceSetting], error) {
	if s == nil || s.settings == nil {
		return nil, fmt.Errorf("config sqlite handle is not configured: %w", apierrors.ErrInvalidInput)
	}
	return s.settings, nil
}

// loadInstanceRow reads row 1 through the transaction handle. A miss returns
// the default row, never a not-found error: writers merge into it.
func loadInstanceRow(ctx context.Context, transaction *gorm.DB) (record.InstanceSetting, error) {
	row, err := gorm.G[record.InstanceSetting](transaction, clause.Eq{Column: "id", Value: instanceRowID}).First(ctx)
	if err != nil {
		if errors.Is(db.Translate(err), apierrors.ErrNotFound) {
			return record.InstanceSetting{ID: instanceRowID, ModelsFilter: "all"}, nil
		}
		return record.InstanceSetting{}, fmt.Errorf("get instance settings: %w", db.Translate(err))
	}
	return row, nil
}

// loadBootstrapped reads the bootstrapped flag through the transaction handle.
func loadBootstrapped(ctx context.Context, transaction *gorm.DB) (bool, error) {
	row, err := loadInstanceRow(ctx, transaction)
	if err != nil {
		return false, err
	}
	return row.Bootstrapped, nil
}

// upsertInstanceRow writes one row through the transaction handle. The
// conflict target is the primary key column only, so constraint violations
// surface as translated errors instead of overwrites.
func upsertInstanceRow(ctx context.Context, transaction *gorm.DB, row record.InstanceSetting) error {
	if err := gorm.G[record.InstanceSetting](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(ctx, &row); err != nil {
		return fmt.Errorf("upsert instance settings: %w", db.Translate(err))
	}
	return nil
}

// toInstanceRecord converts the wire type to the persistence row. An empty
// models filter stores as "all": the column is not null and the CHECK
// accepts all, enabled, disabled only.
func toInstanceRecord(configuration models.RouterConfiguration, bootstrapped bool, updatedAt time.Time) record.InstanceSetting {
	filter := configuration.ModelsFilter
	if filter == "" {
		filter = "all"
	}
	return record.InstanceSetting{
		ID:               instanceRowID,
		Bootstrapped:     bootstrapped,
		IsClusterNode:    configuration.IsClusterNode,
		DisableTelemetry: configuration.DisableTelemetry,
		ModelsFilter:     filter,
		UpdatedAt:        updatedAt,
	}
}

// toRouterConfiguration converts the persistence row to the wire type.
func toRouterConfiguration(row record.InstanceSetting) models.RouterConfiguration {
	return models.RouterConfiguration{
		IsClusterNode:    row.IsClusterNode,
		DisableTelemetry: row.DisableTelemetry,
		ModelsFilter:     row.ModelsFilter,
	}
}
