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
	onChanged   func(providerID string)
}

// SetOnChanged registers a callback fired after credential add/delete for a provider.
func (s *Service) SetOnChanged(fn func(providerID string)) { s.onChanged = fn }

func (s *Service) notifyChanged(providerID string) {
	if s.onChanged != nil {
		s.onChanged(providerID)
	}
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
	Data       map[string]any
}

// Add validates and persists a new Credential for the given provider.
// Validation dispatches to the Go adapter or the Lua validate_credentials
// handler for the provider's type key.
func (s *Service) Add(opts AddOptions) (*models.Credential, error) {
	resolved, err := provider.Resolve(s.providerSvc, opts.ProviderID)
	if err != nil {
		return nil, err
	}
	data := opts.Data
	if data == nil {
		data = map[string]any{}
	}
	if resolved.IsLua() {
		ok, verr := s.providerSvc.LuaService().ValidateCredentials(resolved.Instance.TypeKey, data)
		if verr != nil {
			return nil, fmt.Errorf("invalid credentials: %w", verr)
		}
		if !ok {
			return nil, fmt.Errorf("invalid credentials: rejected by provider")
		}
	} else {
		if err := resolved.Go.ValidateCredentials(data); err != nil {
			return nil, fmt.Errorf("invalid credentials: %w", err)
		}
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
		Data:       data,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.repo.Put(id, cred); err != nil {
		return nil, err
	}
	s.notifyChanged(opts.ProviderID)
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

// All returns routable credentials for a provider: non-expired and not
// disabled, sorted by manual order, then computed priority, then LRU.
func (s *Service) All(providerID string) ([]*models.Credential, error) {
	all, err := s.repo.ListFiltered(func(c *models.Credential) bool {
		return c.ProviderID == providerID
	})
	if err != nil {
		return nil, err
	}

	available := make([]*models.Credential, 0, len(all))
	for _, c := range all {
		if !c.IsExpired() && !c.Disabled {
			available = append(available, c)
		}
	}

	if len(available) == 0 {
		return nil, fmt.Errorf("no available credentials for provider %s", providerID)
	}

	SortPool(available)

	return available, nil
}

// SortPool orders a credential pool: manually ordered first (Order asc),
// then by computed priority, then least recently used.
func SortPool(creds []*models.Credential) {
	sort.Slice(creds, func(i, j int) bool {
		oi, oj := creds[i].Order, creds[j].Order
		if (oi > 0) != (oj > 0) {
			return oi > 0
		}
		if oi > 0 && oj > 0 && oi != oj {
			return oi < oj
		}
		pi, pj := creds[i].Priority(), creds[j].Priority()
		if pi != pj {
			return pi < pj
		}
		if creds[i].LastUsedAt == nil {
			return true
		}
		if creds[j].LastUsedAt == nil {
			return false
		}
		return creds[i].LastUsedAt.Before(*creds[j].LastUsedAt)
	})
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
func (s *Service) Update(id string, data map[string]any, expiresAt *time.Time) error {
	return s.repo.Update(id, func(c *models.Credential) error {
		c.Data = data
		c.ExpiresAt = expiresAt
		c.UpdatedAt = util.Now()
		return nil
	})
}

// UpdateDetails edits the admin-facing fields: label, enable/disable, and
// optionally the credential data (revalidated against the provider backend
// when replaced).
func (s *Service) UpdateDetails(id string, label *string, disabled *bool, data map[string]any) error {
	cred, err := s.repo.Get(id)
	if err != nil {
		return err
	}
	if data != nil {
		resolved, err := provider.Resolve(s.providerSvc, cred.ProviderID)
		if err != nil {
			return err
		}
		if resolved.IsLua() {
			ok, verr := s.providerSvc.LuaService().ValidateCredentials(resolved.Instance.TypeKey, data)
			if verr != nil {
				return fmt.Errorf("invalid credentials: %w", verr)
			}
			if !ok {
				return fmt.Errorf("invalid credentials: rejected by provider")
			}
		} else if err := resolved.Go.ValidateCredentials(data); err != nil {
			return fmt.Errorf("invalid credentials: %w", err)
		}
	}
	if err := s.repo.Update(id, func(c *models.Credential) error {
		if label != nil {
			c.Label = *label
		}
		if disabled != nil {
			c.Disabled = *disabled
		}
		if data != nil {
			c.Data = data
		}
		c.UpdatedAt = util.Now()
		return nil
	}); err != nil {
		return err
	}
	if disabled != nil {
		s.notifyChanged(cred.ProviderID)
	}
	return nil
}

// Reorder sets the manual pool order for a provider's credentials.
// ids must cover every credential of the provider; Order becomes 1-based.
func (s *Service) Reorder(providerID string, ids []string) error {
	creds, err := s.ListByProvider(providerID)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(creds))
	for _, c := range creds {
		known[c.ID] = true
	}
	if len(ids) != len(creds) {
		return fmt.Errorf("reorder list must contain all %d credentials of provider %s", len(creds), providerID)
	}
	seen := make(map[string]bool, len(ids))
	for i, id := range ids {
		if !known[id] {
			return fmt.Errorf("credential %q does not belong to provider %s", id, providerID)
		}
		if seen[id] {
			return fmt.Errorf("credential %q listed twice", id)
		}
		seen[id] = true
		order := i + 1
		if err := s.repo.Update(id, func(c *models.Credential) error {
			c.Order = order
			c.UpdatedAt = util.Now()
			return nil
		}); err != nil {
			return err
		}
	}
	s.notifyChanged(providerID)
	return nil
}

// Delete removes a Credential by ID.
func (s *Service) Delete(id string) error {
	cred, _ := s.repo.Get(id)
	providerID := ""
	if cred != nil {
		providerID = cred.ProviderID
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	if providerID != "" {
		s.notifyChanged(providerID)
	}
	return nil
}

// DeleteByProvider removes every Credential of one provider.
// Returns the removed count.
func (s *Service) DeleteByProvider(providerID string) (int, error) {
	creds, err := s.ListByProvider(providerID)
	if err != nil {
		return 0, err
	}
	for _, c := range creds {
		if err := s.repo.Delete(c.ID); err != nil {
			return 0, err
		}
	}
	if len(creds) > 0 {
		s.notifyChanged(providerID)
	}
	return len(creds), nil
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
