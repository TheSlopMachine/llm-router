// Package token implements the Internal Token Service.
//
// Responsibilities:
//   - CRUD for RouterTokens
//   - Validation of incoming bearer tokens against stored hashes
//   - Enforcement of TokenRules
package token

import (
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// Service manages router-tokens.
type Service struct {
	db   *db.DB
	repo *repository.Repository[models.RouterToken]
}

// New constructs a new token Service.
func New(database *db.DB) *Service {
	return &Service{
		db:   database,
		repo: repository.New[models.RouterToken](database, db.BucketTokens, "token"),
	}
}

// ─────────────────────────────────────────────
// Creation
// ─────────────────────────────────────────────

// CreateOptions holds the parameters for issuing a new token.
type CreateOptions struct {
	Name  string
	Rules models.TokenRules
}

// Create mints a new RouterToken and persists it.
// The raw (unhashed) token value is returned exactly once — it is not stored.
func (s *Service) Create(opts CreateOptions) (*models.RouterToken, error) {
	raw, err := util.GenerateToken()
	if err != nil {
		return nil, fmt.Errorf("generate token secret: %w", err)
	}

	id, err := util.GenerateID()
	if err != nil {
		return nil, fmt.Errorf("generate token ID: %w", err)
	}

	t := &models.RouterToken{
		ID:        id,
		Name:      opts.Name,
		Token:     raw, // returned to caller, not stored
		TokenHash: util.HashSecret(raw),
		Rules:     opts.Rules,
		CreatedAt: util.Now(),
	}

	if err := s.put(t); err != nil {
		return nil, err
	}
	return t, nil
}

// ─────────────────────────────────────────────
// Validation
// ─────────────────────────────────────────────

// Validate looks up the RouterToken matching the provided raw bearer value.
// Returns ErrUnauthorized if the token is not found.
func (s *Service) Validate(raw string) (*models.RouterToken, error) {
	hash := util.HashSecret(raw)

	var token *models.RouterToken
	err := s.db.View(func(tx *bolt.Tx) error {
		// token_index: hash → id
		idx := tx.Bucket(db.BucketTokenIndex)
		id := idx.Get([]byte(hash))
		if id == nil {
			return errors.ErrUnauthorized
		}

		data := tx.Bucket(db.BucketTokens).Get(id)
		if data == nil {
			return errors.ErrUnauthorized
		}

		token = &models.RouterToken{}
		return json.Unmarshal(data, token)
	})
	return token, err
}

// ─────────────────────────────────────────────
// CRUD
// ─────────────────────────────────────────────

// Get returns a RouterToken by ID.
func (s *Service) Get(id string) (*models.RouterToken, error) {
	return s.repo.Get(id)
}

// List returns all RouterTokens.
func (s *Service) List() ([]*models.RouterToken, error) {
	return s.repo.List()
}

// Delete removes a RouterToken and its index entry by ID.
func (s *Service) Delete(id string) error {
	// Get token first to retrieve hash for index cleanup
	t, err := s.repo.Get(id)
	if err != nil {
		return err
	}

	// Delete from both buckets
	return s.db.Update(func(tx *bolt.Tx) error {
		// Remove from index
		if err := tx.Bucket(db.BucketTokenIndex).Delete([]byte(t.TokenHash)); err != nil {
			return err
		}
		// Remove from main bucket
		return tx.Bucket(db.BucketTokens).Delete([]byte(id))
	})
}

// UpdateRules replaces the TokenRules for an existing token.
func (s *Service) UpdateRules(id string, rules models.TokenRules) error {
	return s.repo.Update(id, func(t *models.RouterToken) error {
		t.Rules = rules
		return nil
	})
}

// ─────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────

// put persists a token and updates the hash → id index.
func (s *Service) put(t *models.RouterToken) error {
	// Do not persist the raw token value.
	stored := *t
	stored.Token = ""

	if err := s.repo.Put(t.ID, &stored); err != nil {
		return err
	}

	// Update index separately
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(db.BucketTokenIndex).Put([]byte(t.TokenHash), []byte(t.ID))
	})
}

