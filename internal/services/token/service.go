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
	database *db.DB
	store    *repository.Repository[models.RouterToken]
}

// New constructs a new token Service.
func New(database *db.DB) *Service {
	return &Service{
		database: database,
		store:    repository.New[models.RouterToken](database, db.BucketTokens, "token"),
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
func (s *Service) Create(options CreateOptions) (*models.RouterToken, error) {
	raw, err := util.GenerateToken()
	if err != nil {
		return nil, fmt.Errorf("generate token secret: %w", err)
	}

	id, err := util.GenerateID()
	if err != nil {
		return nil, fmt.Errorf("generate token ID: %w", err)
	}

	token := &models.RouterToken{
		ID:        id,
		Name:      options.Name,
		Token:     raw, // returned to caller, not stored
		TokenHash: util.HashSecret(raw),
		Rules:     options.Rules,
		CreatedAt: util.Now(),
	}

	if err := s.put(token); err != nil {
		return nil, err
	}
	return token, nil
}

// ─────────────────────────────────────────────
// Validation
// ─────────────────────────────────────────────

// Validate looks up the RouterToken matching the provided raw bearer value.
// Returns ErrUnauthorized if the token is not found.
func (s *Service) Validate(raw string) (*models.RouterToken, error) {
	hash := util.HashSecret(raw)

	var token *models.RouterToken
	err := s.database.View(func(tx *bolt.Tx) error {
		// token_index: hash → id
		index := tx.Bucket(db.BucketTokenIndex)
		id := index.Get([]byte(hash))
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
	return s.store.Get(id)
}

// List returns all RouterTokens.
func (s *Service) List() ([]*models.RouterToken, error) {
	return s.store.List()
}

// Delete removes a RouterToken and its index entry by ID.
func (s *Service) Delete(id string) error {
	// Get token first to retrieve hash for index cleanup
	token, err := s.store.Get(id)
	if err != nil {
		return err
	}

	// Delete from both buckets
	return s.database.Update(func(tx *bolt.Tx) error {
		// Remove from index
		if err := tx.Bucket(db.BucketTokenIndex).Delete([]byte(token.TokenHash)); err != nil {
			return err
		}
		// Remove from main bucket
		return tx.Bucket(db.BucketTokens).Delete([]byte(id))
	})
}

// UpdateRules replaces the TokenRules for an existing token.
func (s *Service) UpdateRules(id string, rules models.TokenRules) error {
	return s.store.Update(id, func(token *models.RouterToken) error {
		token.Rules = rules
		return nil
	})
}

// Regenerate replaces the secret of an existing token with a new one.
// The token keeps its ID, Name and Rules; the old secret is invalidated.
func (s *Service) Regenerate(id string) (*models.RouterToken, error) {
	raw, err := util.GenerateToken()
	if err != nil {
		return nil, fmt.Errorf("generate token secret: %w", err)
	}
	newHash := util.HashSecret(raw)
	var result *models.RouterToken
	var oldHash string
	err = s.database.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(db.BucketTokens)
		data := bucket.Get([]byte(id))
		if data == nil {
			return fmt.Errorf("token %q: %w", id, errors.ErrNotFound)
		}
		var token models.RouterToken
		if err := json.Unmarshal(data, &token); err != nil {
			return fmt.Errorf("unmarshal token: %w", err)
		}
		oldHash = token.TokenHash
		token.TokenHash = newHash
		encoded, err := json.Marshal(&token)
		if err != nil {
			return fmt.Errorf("marshal token: %w", err)
		}
		if err := bucket.Put([]byte(id), encoded); err != nil {
			return err
		}
		result = &token
		index := tx.Bucket(db.BucketTokenIndex)
		if oldHash != "" {
			if err := index.Delete([]byte(oldHash)); err != nil {
				return err
			}
		}
		return index.Put([]byte(newHash), []byte(id))
	})
	if err != nil {
		return nil, err
	}
	result.Token = raw
	return result, nil
}

// ─────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────

// put persists a token and updates the hash → id index in one transaction.
func (s *Service) put(token *models.RouterToken) error {
	// Do not persist the raw token value.
	stored := *token
	stored.Token = ""
	encoded, err := json.Marshal(&stored)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}
	return s.database.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(db.BucketTokens).Put([]byte(token.ID), encoded); err != nil {
			return err
		}
		return tx.Bucket(db.BucketTokenIndex).Put([]byte(token.TokenHash), []byte(token.ID))
	})
}
