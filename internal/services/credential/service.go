// Package credential implements the Credential Pool Service.
//
// Responsibilities:
//   - Storage and retrieval of provider-specific credentials
//   - Admin mutation (label, enable/disable, data, delete)
//   - Persisting job-refreshed credential data
//
// Selection, rotation, ordering and usage accounting belong to plugins:
// the service keeps no pool order, no LRU state and no usage counters.
package credential

import (
	"fmt"
	"log/slog"
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
	parks       *repository.Repository[models.ParkEntry]
	onChanged   func(providerID string)
	logger      *slog.Logger
}

// SetLogger wires structured logging.
func (s *Service) SetLogger(l *slog.Logger) { s.logger = l }

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
		parks:       repository.New[models.ParkEntry](database, db.BucketCredentialParks, "park"),
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

// ListUsable returns routable credentials for a provider: non-expired and
// not disabled, in stored order. Empty pools yield an empty slice, never
// an error: plugin discovery and dashboard reads handle absence themselves.
// Selection among them belongs to the plugin.
func (s *Service) ListUsable(providerID string) ([]*models.Credential, error) {
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

	return available, nil
}

// All returns routable credentials for a provider, denying empty pools:
// the router gate maps absence to NoCredential before plugins run.
func (s *Service) All(providerID string) ([]*models.Credential, error) {
	available, err := s.ListUsable(providerID)
	if err != nil {
		return nil, err
	}

	if len(available) == 0 {
		return nil, fmt.Errorf("no available credentials for provider %s", providerID)
	}

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
			if *disabled {
				now := util.Now()
				c.DisabledBy = models.DisabledByAdmin
				c.DisabledReason = "disabled by admin"
				c.DisabledAt = &now
			} else {
				c.DisabledBy = ""
				c.DisabledReason = ""
				c.DisabledAt = nil
			}
		}
		if data != nil {
			c.Data = data
		}
		c.UpdatedAt = util.Now()
		return nil
	}); err != nil {
		return err
	}
	if disabled != nil && s.logger != nil {
		if *disabled {
			s.logger.Info("credential disabled by admin", "credential_id", id, "provider_id", cred.ProviderID)
		} else {
			s.logger.Info("credential enabled by admin", "credential_id", id, "provider_id", cred.ProviderID)
		}
	}
	if disabled != nil {
		s.notifyChanged(cred.ProviderID)
	}
	return nil
}

// DisableUnhealthy disables a credential on an explicit unhealthy
// health-check verdict. First wins: an already-disabled credential keeps
// its original cause. Manual admin re-enable clears the flag.
func (s *Service) DisableUnhealthy(id, reason string) error {
	if reason == "" {
		reason = "health check reported unhealthy"
	}
	if len(reason) > 1024 {
		reason = reason[:1024] + "…[truncated]"
	}
	disabled := false
	err := s.repo.Update(id, func(c *models.Credential) error {
		if c.Disabled {
			return nil
		}
		now := util.Now()
		c.Disabled = true
		c.DisabledBy = models.DisabledByHealthcheck
		c.DisabledReason = reason
		c.DisabledAt = &now
		c.UpdatedAt = now
		disabled = true
		return nil
	})
	if err != nil {
		return err
	}
	if disabled && s.logger != nil {
		s.logger.Info("credential disabled by healthcheck", "credential_id", id, "reason", reason)
	}
	if cred, gerr := s.repo.Get(id); gerr == nil && cred != nil {
		s.notifyChanged(cred.ProviderID)
	}
	return nil
}

// DisableByPlugin disables a credential on a plugin's dead-key verdict.
// First wins: an already-disabled credential keeps its original cause, so
// plugin writes never override admin or healthcheck disables. Manual admin
// re-enable clears the flag.
func (s *Service) DisableByPlugin(id, reason string) error {
	if reason == "" {
		reason = "disabled by plugin"
	}
	if len(reason) > 1024 {
		reason = reason[:1024] + "…[truncated]"
	}
	disabled := false
	err := s.repo.Update(id, func(c *models.Credential) error {
		if c.Disabled {
			return nil
		}
		now := util.Now()
		c.Disabled = true
		c.DisabledBy = models.DisabledByPlugin
		c.DisabledReason = reason
		c.DisabledAt = &now
		c.UpdatedAt = now
		disabled = true
		return nil
	})
	if err != nil {
		return err
	}
	if disabled && s.logger != nil {
		s.logger.Info("credential disabled by plugin", "credential_id", id, "reason", reason)
	}
	if cred, gerr := s.repo.Get(id); gerr == nil && cred != nil {
		s.notifyChanged(cred.ProviderID)
	}
	return nil
}

// EnableByPlugin clears a plugin-caused disable when the key proves
// healthy again. Disables owned by admin or healthcheck verdicts are left
// untouched: only the actor that benched the key lifts it, besides the
// manual admin toggle which clears every cause.
func (s *Service) EnableByPlugin(id string) error {
	var cleared bool
	err := s.repo.Update(id, func(c *models.Credential) error {
		if !c.Disabled || c.DisabledBy != models.DisabledByPlugin {
			return nil
		}
		c.Disabled = false
		c.DisabledBy = ""
		c.DisabledReason = ""
		c.DisabledAt = nil
		c.UpdatedAt = util.Now()
		cleared = true
		return nil
	})
	if err != nil {
		return err
	}
	if cleared {
		if cred, gerr := s.repo.Get(id); gerr == nil && cred != nil {
			s.notifyChanged(cred.ProviderID)
		}
	}
	return nil
}

// maxParkTTL bounds unified cooldown parks to 90 days, matching storage.
const maxParkTTL = 90 * 24 * time.Hour

// Park benches a credential in the shared cooldown store until ttl lapses:
// plugin rotation skips it, the dashboard keeps showing it enabled with an
// unpark action. Unknown IDs fail loudly; TTL must be positive and bounded.
func (s *Service) Park(id string, ttl time.Duration, reason string) error {
	if ttl <= 0 {
		return fmt.Errorf("park ttl must be positive")
	}
	if ttl > maxParkTTL {
		return fmt.Errorf("park ttl exceeds 90 days")
	}
	if _, err := s.repo.Get(id); err != nil {
		return err
	}
	now := util.Now()
	return s.parks.Put(id, &models.ParkEntry{
		CredentialID: id,
		Reason:       reason,
		Until:        now.Add(ttl),
		ParkedAt:     now,
	})
}

// Unpark clears a cooldown park so the credential serves immediately.
// Missing parks succeed: unparking is idempotent.
func (s *Service) Unpark(id string) error {
	return s.parks.DeleteIfExists(id)
}

// Parked returns the live park entry for a credential, or nil when absent
// or lapsed. Lapsed rows delete on read.
func (s *Service) Parked(id string) (*models.ParkEntry, error) {
	entry, err := s.parks.Get(id)
	if err != nil {
		return nil, nil
	}
	if entry == nil || entry.Expired(util.Now()) {
		_ = s.parks.DeleteIfExists(id)
		return nil, nil
	}
	return entry, nil
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
