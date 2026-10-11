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
