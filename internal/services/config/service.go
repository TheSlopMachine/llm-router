// Package config manages RouterConfiguration persisted in the
// instance_settings row: the configuration columns plus the bootstrapped
// flag.
package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
)

// instanceRowID pins the single instance_settings row. The table CHECK
// enforces id = 1; the service never reads or writes another identifier.
const instanceRowID int64 = 1

// Service manages RouterConfiguration.
type Service struct {
	sqlite   *db.Database
	settings *repository.GormRepository[record.InstanceSetting]
	logger   *slog.Logger
}

// New constructs a config Service on the SQLite handle.
func New(sqlite *db.Database) *Service {
	service := &Service{sqlite: sqlite}
	if sqlite != nil && sqlite.Handle != nil {
		service.settings = repository.NewGormRepository[record.InstanceSetting](sqlite.Handle, "id")
	}
	return service
}

// SetLogger wires structured logging.
func (s *Service) SetLogger(l *slog.Logger) { s.logger = l }

// GetSettings returns the stored RouterConfiguration from instance_settings.
// A missing row returns the zero configuration, matching the previous behavior.
func (s *Service) GetSettings(ctx context.Context) (models.RouterConfiguration, error) {
	settings, err := s.requireSettings()
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Get settings failed", "error", err)
		}
		return models.RouterConfiguration{}, err
	}
	row, err := settings.Get(ctx, clause.Eq{Column: "id", Value: instanceRowID})
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			if s.logger != nil {
				s.logger.Debug("Settings not found, using defaults")
			}
			return models.RouterConfiguration{}, nil
		}
		if s.logger != nil {
			s.logger.Error("Get settings failed", "error", err)
		}
		return models.RouterConfiguration{}, fmt.Errorf("get instance settings: %w", err)
	}
	configuration := toRouterConfiguration(row)
	if s.logger != nil {
		s.logger.Debug("Settings retrieved", "models_filter", configuration.ModelsFilter, "is_cluster_node", configuration.IsClusterNode, "disable_telemetry", configuration.DisableTelemetry)
	}
	return configuration, nil
}

// PutSettings validates and persists RouterConfiguration to
// instance_settings. The bootstrapped flag survives the write: one
// transaction reads the row and upserts the merged record through the
// transaction handle only.
func (s *Service) PutSettings(ctx context.Context, configuration models.RouterConfiguration) error {
	if err := configuration.Validate(); err != nil {
		if s.logger != nil {
			s.logger.Warn("Validate router configuration failed", "error", err)
		}
		return fmt.Errorf("validate router configuration: %w", err)
	}
	sqlite, err := s.requireDatabase()
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Put settings failed", "error", err)
		}
		return err
	}
	var preserved bool
	if err := sqlite.Handle.Transaction(func(transaction *gorm.DB) error {
		bootstrapped, err := loadBootstrapped(ctx, transaction)
		if err != nil {
			return err
		}
		preserved = bootstrapped
		return upsertInstanceRow(ctx, transaction, toInstanceRecord(configuration, bootstrapped, time.Now().UTC()))
	}); err != nil {
		translated := db.Translate(err)
		if s.logger != nil {
			s.logger.Error("Put settings failed", "models_filter", configuration.ModelsFilter, "error", translated)
		}
		return translated
	}
	if s.logger != nil {
		s.logger.Info("Settings updated", "models_filter", configuration.ModelsFilter, "is_cluster_node", configuration.IsClusterNode, "disable_telemetry", configuration.DisableTelemetry, "bootstrapped", preserved)
	}
	return nil
}

// IsBootstrapped reports the instance_settings bootstrapped flag. A missing
// row reads false: unknown state never reports bootstrapped.
func (s *Service) IsBootstrapped(ctx context.Context) (bool, error) {
	settings, err := s.requireSettings()
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Check bootstrapped failed", "error", err)
		}
		return false, err
	}
	row, err := settings.Get(ctx, clause.Eq{Column: "id", Value: instanceRowID})
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			if s.logger != nil {
				s.logger.Debug("Bootstrapped flag not found, using default")
			}
			return false, nil
		}
		if s.logger != nil {
			s.logger.Error("Check bootstrapped failed", "error", err)
		}
		return false, fmt.Errorf("get bootstrapped flag: %w", err)
	}
	if s.logger != nil {
		s.logger.Debug("Bootstrapped flag checked", "bootstrapped", row.Bootstrapped)
	}
	return row.Bootstrapped, nil
}

// SetBootstrapped marks the instance bootstrapped and preserves the stored
// configuration columns. One transaction, transaction handle only.
func (s *Service) SetBootstrapped(ctx context.Context) error {
	sqlite, err := s.requireDatabase()
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Set bootstrapped failed", "error", err)
		}
		return err
	}
	firstTransition := false
	if err := sqlite.Handle.Transaction(func(transaction *gorm.DB) error {
		row, err := loadInstanceRow(ctx, transaction)
		if err != nil {
			return err
		}
		firstTransition = !row.Bootstrapped
		row.Bootstrapped = true
		row.UpdatedAt = time.Now().UTC()
		return upsertInstanceRow(ctx, transaction, row)
	}); err != nil {
		translated := db.Translate(err)
		if s.logger != nil {
			s.logger.Error("Set bootstrapped failed", "error", translated)
		}
		return translated
	}
	if s.logger != nil {
		if firstTransition {
			s.logger.Info("Instance bootstrapped")
		} else {
			s.logger.Debug("Instance already bootstrapped")
		}
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
