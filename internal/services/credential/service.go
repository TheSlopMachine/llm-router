// Package credential implements the Credential Pool Service.
//
// Responsibilities:
//   - Storage and retrieval of provider-specific credentials
//   - LRU-based credential selection with priority ordering
//   - Usage tracking and quota management
//   - Updating credentials after refresh by the Maintenance service
package credential

import (
	"fmt"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// Service manages Credential records.
type Service struct {
	db          *db.DB
	providerSvc *provider.Service
	repo        *repository.Repository[models.Credential]
}

// New constructs a new credential Service.
func New(database *db.DB, providerSvc *provider.Service) *Service {
	return &Service{
		db:          database,
		providerSvc: providerSvc,
		repo:        repository.New[models.Credential](database, db.BucketCredentials, "credential"),
	}
}

// ─────────────────────────────────────────────
// Creation
// ─────────────────────────────────────────────

// AddOptions holds parameters for adding a new credential to a provider's pool.
type AddOptions struct {
	ProviderID string
	Label      string
	Data       map[string]string
}

// Add validates and persists a new Credential for the given provider.
func (s *Service) Add(opts AddOptions) (*models.Credential, error) {
	adapter, _, err := provider.ResolveAdapter(s.providerSvc, opts.ProviderID)
	if err != nil {
		return nil, err
	}

	if err := adapter.ValidateCredentials(opts.Data); err != nil {
		return nil, fmt.Errorf("invalid credentials: %w", err)
	}

	id, err := util.GenerateID()
	if err != nil {
		return nil, err
	}

	now := util.Now()
	cred := &models.Credential{
		ID:         id,
		ProviderID: opts.ProviderID,
		Label:      opts.Label,
		Data:       opts.Data,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.repo.Put(id, cred); err != nil {
		return nil, err
	}
	return cred, nil
}

// ─────────────────────────────────────────────
// Retrieval
// ─────────────────────────────────────────────

// Get returns a single Credential by ID.
func (s *Service) Get(id string) (*models.Credential, error) {
	return s.repo.Get(id)
}

// Next returns the best available (non-expired) Credential for a provider.
// Selection strategy: LRU with priority ordering.
// Priority: 0 (never used) > 1 (normal) > 2 (quota exceeded)
// Expired credentials (priority 3) are never returned.
func (s *Service) Next(providerID string) (*models.Credential, error) {
	creds, err := s.All(providerID)
	if err != nil {
		return nil, err
	}
	if len(creds) == 0 {
		return nil, fmt.Errorf("no available credentials for provider %s", providerID)
	}
	return creds[0], nil
}

// All returns all non-expired credentials for a provider, sorted by priority and LRU.
func (s *Service) All(providerID string) ([]*models.Credential, error) {
	all, err := s.repo.ListFiltered(func(c *models.Credential) bool {
		return c.ProviderID == providerID
	})
	if err != nil {
		return nil, err
	}

	// Filter out expired credentials
	available := make([]*models.Credential, 0, len(all))
	for _, c := range all {
		if !c.IsExpired() {
			available = append(available, c)
		}
	}

	if len(available) == 0 {
		return nil, fmt.Errorf("all credentials for provider %s are expired", providerID)
	}

	// Sort by priority, then by LRU
	sort.Slice(available, func(i, j int) bool {
		pi, pj := available[i].Priority(), available[j].Priority()
		if pi != pj {
			return pi < pj // Lower priority number = higher priority
		}
		// Same priority: sort by LRU (nil = never used = highest priority)
		if available[i].LastUsedAt == nil {
			return true
		}
		if available[j].LastUsedAt == nil {
			return false
		}
		return available[i].LastUsedAt.Before(*available[j].LastUsedAt)
	})

	return available, nil
}

// ListByProvider returns all Credentials for a given provider.
func (s *Service) ListByProvider(providerID string) ([]*models.Credential, error) {
	return s.repo.ListFiltered(func(c *models.Credential) bool {
		return c.ProviderID == providerID
	})
}

// ListAll returns every Credential across all providers.
func (s *Service) ListAll() ([]*models.Credential, error) {
	return s.repo.List()
}

// ─────────────────────────────────────────────
// Mutation
// ─────────────────────────────────────────────

// Update replaces a Credential's mutable fields (data, expiry).
// Used by the Maintenance service after a successful token refresh.
func (s *Service) Update(id string, data map[string]string, expiresAt *time.Time) error {
	return s.repo.Update(id, func(c *models.Credential) error {
		c.Data = data
		c.ExpiresAt = expiresAt
		c.UpdatedAt = util.Now()
		return nil
	})
}

// Delete removes a Credential by ID.
func (s *Service) Delete(id string) error {
	return s.repo.Delete(id)
}

// ─────────────────────────────────────────────
// Usage Tracking
// ─────────────────────────────────────────────

// UpdateUsage updates credential usage statistics after a request.
func (s *Service) UpdateUsage(id string, success bool) error {
	return s.repo.Update(id, func(c *models.Credential) error {
		c.IncrementUsage(success)
		c.UpdatedAt = util.Now()
		return nil
	})
}

// MarkQuotaExceeded marks a credential as quota-exceeded until resetAt.
func (s *Service) MarkQuotaExceeded(id string, resetAt time.Time) error {
	return s.repo.Update(id, func(c *models.Credential) error {
		c.MarkQuotaExceeded(resetAt)
		c.UpdatedAt = util.Now()
		return nil
	})
}

