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
	database        *db.DB
	providerService *provider.Service
	store           *repository.Repository[models.Credential]
	parks           *repository.Repository[models.ParkEntry]
	onChanged       func(providerID string)
	logger          *slog.Logger
}

// SetLogger wires structured logging.
func (s *Service) SetLogger(l *slog.Logger) { s.logger = l }

// SetOnChanged registers a callback fired after credential add/delete for a provider.
func (s *Service) SetOnChanged(function func(providerID string)) { s.onChanged = function }

func (s *Service) notifyChanged(providerID string) {
	if s.onChanged != nil {
		s.onChanged(providerID)
	}
}

// New constructs a new credential Service.
func New(database *db.DB, providerService *provider.Service) *Service {
	return &Service{
		database:        database,
		providerService: providerService,
		store:           repository.New[models.Credential](database, db.BucketCredentials, "credential"),
		parks:           repository.New[models.ParkEntry](database, db.BucketCredentialParks, "park"),
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
func (s *Service) Add(options AddOptions) (*models.Credential, error) {
	resolved, err := provider.Resolve(s.providerService, options.ProviderID)
	if err != nil {
		return nil, err
	}
	data := options.Data
	if data == nil {
		data = map[string]any{}
	}
	if resolved.IsLua() {
		ok, validateError := s.providerService.LuaService().ValidateCredentials(resolved.Instance.TypeKey, data)
		if validateError != nil {
			return nil, fmt.Errorf("invalid credentials: %w", validateError)
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
	credential := &models.Credential{
		ID:         id,
		ProviderID: options.ProviderID,
		Label:      options.Label,
		Data:       data,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.store.Put(id, credential); err != nil {
		return nil, err
	}
	s.notifyChanged(options.ProviderID)
	return credential, nil
}

// ─────────────────────────────────────────────
// Retrieval
// ─────────────────────────────────────────────

// Get returns a single Credential by ID.
func (s *Service) Get(id string) (*models.Credential, error) {
	return s.store.Get(id)
}

// ListUsable returns routable credentials for a provider: non-expired and
// not disabled, in stored order. Empty pools yield an empty slice, never
// an error: plugin discovery and dashboard reads handle absence themselves.
// Selection among them belongs to the plugin.
func (s *Service) ListUsable(providerID string) ([]*models.Credential, error) {
	all, err := s.store.ListFiltered(func(c *models.Credential) bool {
		return c.ProviderID == providerID
	})
	if err != nil {
		return nil, err
	}

	available := make([]*models.Credential, 0, len(all))
	for _, credential := range all {
		if !credential.IsExpired() && !credential.Disabled {
			available = append(available, credential)
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
	return s.store.ListFiltered(func(c *models.Credential) bool {
		return c.ProviderID == providerID
	})
}

// ListAll returns every Credential across all providers.
func (s *Service) ListAll() ([]*models.Credential, error) {
	return s.store.List()
}

// ─────────────────────────────────────────────
// Mutation
// ─────────────────────────────────────────────

// Update replaces a Credential's mutable fields (data, expiry).
func (s *Service) Update(id string, data map[string]any, expiresAt *time.Time) error {
	return s.store.Update(id, func(credential *models.Credential) error {
		credential.Data = data
		credential.ExpiresAt = expiresAt
		credential.UpdatedAt = util.Now()
		return nil
	})
}

// UpdateDetails edits the admin-facing fields: label, enable/disable, and
// optionally the credential data (revalidated against the provider backend
// when replaced).
func (s *Service) UpdateDetails(id string, label *string, disabled *bool, data map[string]any) error {
	credential, err := s.store.Get(id)
	if err != nil {
		return err
	}
	if data != nil {
		resolved, err := provider.Resolve(s.providerService, credential.ProviderID)
		if err != nil {
			return err
		}
		if resolved.IsLua() {
			ok, validateError := s.providerService.LuaService().ValidateCredentials(resolved.Instance.TypeKey, data)
			if validateError != nil {
				return fmt.Errorf("invalid credentials: %w", validateError)
			}
			if !ok {
				return fmt.Errorf("invalid credentials: rejected by provider")
			}
		} else if err := resolved.Go.ValidateCredentials(data); err != nil {
			return fmt.Errorf("invalid credentials: %w", err)
		}
	}
	if err := s.store.Update(id, func(current *models.Credential) error {
		if label != nil {
			current.Label = *label
		}
		if disabled != nil {
			current.Disabled = *disabled
			if *disabled {
				now := util.Now()
				current.DisabledBy = models.DisabledByAdmin
				current.DisabledReason = "disabled by admin"
				current.DisabledAt = &now
			} else {
				current.DisabledBy = ""
				current.DisabledReason = ""
				current.DisabledAt = nil
			}
		}
		if data != nil {
			current.Data = data
		}
		current.UpdatedAt = util.Now()
		return nil
	}); err != nil {
		return err
	}
	if disabled != nil && s.logger != nil {
		if *disabled {
			s.logger.Info("credential disabled by admin", "credential_id", id, "provider_id", credential.ProviderID)
		} else {
			s.logger.Info("credential enabled by admin", "credential_id", id, "provider_id", credential.ProviderID)
		}
	}
	if disabled != nil {
		s.notifyChanged(credential.ProviderID)
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
	err := s.store.Update(id, func(credential *models.Credential) error {
		if credential.Disabled {
			return nil
		}
		now := util.Now()
		credential.Disabled = true
		credential.DisabledBy = models.DisabledByHealthcheck
		credential.DisabledReason = reason
		credential.DisabledAt = &now
		credential.UpdatedAt = now
		disabled = true
		return nil
	})
	if err != nil {
		return err
	}
	if disabled && s.logger != nil {
		s.logger.Info("credential disabled by healthcheck", "credential_id", id, "reason", reason)
	}
	if credential, getError := s.store.Get(id); getError == nil && credential != nil {
		s.notifyChanged(credential.ProviderID)
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
	err := s.store.Update(id, func(credential *models.Credential) error {
		if credential.Disabled {
			return nil
		}
		now := util.Now()
		credential.Disabled = true
		credential.DisabledBy = models.DisabledByPlugin
		credential.DisabledReason = reason
		credential.DisabledAt = &now
		credential.UpdatedAt = now
		disabled = true
		return nil
	})
	if err != nil {
		return err
	}
	if disabled && s.logger != nil {
		s.logger.Info("credential disabled by plugin", "credential_id", id, "reason", reason)
	}
	if credential, getError := s.store.Get(id); getError == nil && credential != nil {
		s.notifyChanged(credential.ProviderID)
	}
	return nil
}

// EnableByPlugin clears a plugin-caused disable when the key proves
// healthy again. Disables owned by admin or healthcheck verdicts are left
// untouched: only the actor that benched the key lifts it, besides the
// manual admin toggle which clears every cause.
func (s *Service) EnableByPlugin(id string) error {
	var cleared bool
	err := s.store.Update(id, func(credential *models.Credential) error {
		if !credential.Disabled || credential.DisabledBy != models.DisabledByPlugin {
			return nil
		}
		credential.Disabled = false
		credential.DisabledBy = ""
		credential.DisabledReason = ""
		credential.DisabledAt = nil
		credential.UpdatedAt = util.Now()
		cleared = true
		return nil
	})
	if err != nil {
		return err
	}
	if cleared {
		if credential, getError := s.store.Get(id); getError == nil && credential != nil {
			s.notifyChanged(credential.ProviderID)
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
	if _, err := s.store.Get(id); err != nil {
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
	credential, _ := s.store.Get(id)
	providerID := ""
	if credential != nil {
		providerID = credential.ProviderID
	}
	if err := s.store.Delete(id); err != nil {
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
	credentials, err := s.ListByProvider(providerID)
	if err != nil {
		return 0, err
	}
	for _, credential := range credentials {
		if err := s.store.Delete(credential.ID); err != nil {
			return 0, err
		}
	}
	if len(credentials) > 0 {
		s.notifyChanged(providerID)
	}
	return len(credentials), nil
}
