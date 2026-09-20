// Package config manages RouterConfiguration persisted in the RouterConfiguration bucket.
package config

import (
	"encoding/json"
	"fmt"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	bolt "go.etcd.io/bbolt"
)

const instanceKey = "instance"

// Service manages RouterConfiguration.
type Service struct {
	database  *db.DB
	onChanged func(models.RouterConfiguration)
}

// New constructs a config Service.
func New(database *db.DB) *Service {
	return &Service{database: database}
}

// SetOnChanged registers a callback invoked after a successful Put.
func (s *Service) SetOnChanged(fn func(models.RouterConfiguration)) { s.onChanged = fn }

// Get returns the stored RouterConfiguration. Fields that were never
// persisted (old rows predate them) fall back to defaults: zero is never a
// valid value for the ranged fields, so normalizing it is safe.
func (s *Service) Get() (models.RouterConfiguration, error) {
	var cfg models.RouterConfiguration
	err := s.database.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketRouterConfiguration)
		if b == nil {
			return nil
		}
		v := b.Get([]byte(instanceKey))
		if v == nil {
			return nil
		}
		return json.Unmarshal(v, &cfg)
	})
	if err != nil {
		return models.RouterConfiguration{}, err
	}
	if cfg.MinDownloadSpeedKbps <= 0 {
		cfg.MinDownloadSpeedKbps = models.DefaultMinDownloadSpeedKbps
	}
	if cfg.MaxProxiesPerLocation <= 0 {
		cfg.MaxProxiesPerLocation = models.DefaultMaxProxiesPerLocation
	}
	if cfg.UpdateIntervalMinutes <= 0 {
		cfg.UpdateIntervalMinutes = models.DefaultUpdateIntervalMinutes
	}
	return cfg, nil
}

// Put validates and persists RouterConfiguration.
func (s *Service) Put(cfg models.RouterConfiguration) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	enc, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal RouterConfiguration: %w", err)
	}
	if err := s.database.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(db.BucketRouterConfiguration)
		if err != nil {
			return err
		}
		return b.Put([]byte(instanceKey), enc)
	}); err != nil {
		return err
	}
	if s.onChanged != nil {
		s.onChanged(cfg)
	}
	return nil
}
