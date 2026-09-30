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
	database *db.DB
}

// New constructs a config Service.
func New(database *db.DB) *Service {
	return &Service{database: database}
}

// Get returns the stored RouterConfiguration.
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
	return nil
}
