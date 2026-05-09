// Package auth implements ephemeral storage for auth flows.
package auth

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// Service implements provider.AuthStore.
type Service struct {
	db *db.DB
}

// New constructs a new Auth Service.
func New(database *db.DB) *Service {
	return &Service{db: database}
}

type entry struct {
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"created_at"`
}

// Set stores a value with a creation timestamp.
func (s *Service) Set(key, value string) error {
	enc, err := json.Marshal(entry{
		Value:     value,
		CreatedAt: util.Now(),
	})
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(db.BucketAuth).Put([]byte(key), enc)
	})
}

// Get retrieves a value.
func (s *Service) Get(key string) (string, error) {
	var e entry
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(db.BucketAuth).Get([]byte(key))
		if data == nil {
			return fmt.Errorf("key not found")
		}
		return json.Unmarshal(data, &e)
	})
	if err != nil {
		return "", err
	}
	return e.Value, nil
}

// Delete removes a value.
func (s *Service) Delete(key string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(db.BucketAuth).Delete([]byte(key))
	})
}

