package healthcheck

import (
	"context"
	"log/slog"
	"sync"
	"time"

	stdErrors "errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
)

// GormService mirrors Service on the credential_health table. Dispatch keeps
// the bbolt semantics (cooldown window, singleflight, automation-gated
// disable) so cutover swaps the constructor only. The bbolt Service stays
// until Wave 10.
type GormService struct {
	database        *db.Database
	lua             PluginHealth
	credentialStore CredentialStore
	providerConfigs ProviderConfigs
	logger          *slog.Logger

	mu       sync.Mutex
	inflight map[string]bool
}

// NewGormService constructs a GormService on the SQLite handle. lua and
// credentialStore may be nil (checks disabled); providerConfigs may be nil
// (automation switch reads off). Construction performs no I/O. SetLogger
// wires structured logging.
func NewGormService(database *db.Database, lua PluginHealth, credentialStore CredentialStore, providerConfigs ProviderConfigs) *GormService {
	return &GormService{
		database:        database,
		lua:             lua,
		credentialStore: credentialStore,
		providerConfigs: providerConfigs,
		inflight:        map[string]bool{},
	}
}

// SetLogger wires structured logging.
func (s *GormService) SetLogger(l *slog.Logger) { s.logger = l }

// Get returns one credential's health state. A miss returns the not-found
// sentinel.
func (s *GormService) Get(ctx context.Context, credentialID string) (*Record, error) {
	store := repository.NewGormRepository[record.CredentialHealth](s.database.Handle, "credential_id")
	row, err := store.Get(ctx, clause.Eq{Column: clause.Column{Name: "credential_id"}, Value: credentialID})
	if err != nil {
		if stdErrors.Is(err, apierrors.ErrNotFound) {
			return nil, apierrors.NewNotFoundError("credential health", credentialID)
		}
		return nil, err
	}
	return fromHealthRow(row), nil
}

// Stamp records a health verdict for one credential with the current UTC
// time: one public method, one transaction. Repeat stamps overwrite,
// matching the bbolt Put behavior. The credential row must exist; a missing
// credential fails through the foreign key as invalid input.
func (s *GormService) Stamp(ctx context.Context, credentialID, status string) error {
	if credentialID == "" {
		return stdErrors.New("credential health id is required")
	}
	row := toHealthRow(&Record{
		CredentialID: credentialID,
		LastCheckAt:  time.Now().UTC(),
		LastStatus:   status,
	})
	return s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		store := repository.NewGormRepository[record.CredentialHealth](transaction, "credential_id")
		return store.Upsert(ctx, row)
	})
}

// SuspectFailed records a failed attempt identity and starts a detached
// health check unless the type has no check_health handler, a check ran
// within the cooldown window, or one is already in flight. Never blocks;
// never fails the caller.
func (s *GormService) SuspectFailed(pluginID, typeKey, credentialID string) {
	if s.lua == nil || s.credentialStore == nil || credentialID == "" {
		return
	}
	if !s.lua.HasHandler(typeKey, string(luaplugin.HandlerCheckHealth)) {
		return
	}
	cooldown := s.lua.HealthCooldown(typeKey)
	if entry, err := s.Get(context.Background(), credentialID); err == nil && entry != nil {
		if time.Now().Before(entry.LastCheckAt.Add(cooldown)) {
			return
		}
	}
	s.mu.Lock()
	if s.inflight[credentialID] {
		s.mu.Unlock()
		return
	}
	s.inflight[credentialID] = true
	s.mu.Unlock()
	go s.run(typeKey, credentialID)
}

func (s *GormService) done(credentialID string) {
	s.mu.Lock()
	delete(s.inflight, credentialID)
	s.mu.Unlock()
}

// automationOn reports whether the credential's provider opts into
// automatic disables. Absent providers, missing services and absent flags
// read off, matching the dashboard default.
func (s *GormService) automationOn(credential *models.Credential) bool {
	if s.providerConfigs == nil || credential == nil || credential.ProviderID == "" {
		return false
	}
	instance, err := s.providerConfigs.Get(credential.ProviderID)
	if err != nil || instance == nil {
		return false
	}
	return models.CredentialAutomationOn(instance.Config)
}

// run executes one detached health check: fetch the credential, invoke the
// handler with a bounded timeout, stamp the outcome, and disable only on
// an explicit unhealthy verdict with the provider automation switch on.
func (s *GormService) run(typeKey, credentialID string) {
	defer s.done(credentialID)
	credential, err := s.credentialStore.Get(credentialID)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), CheckTimeout)
	defer cancel()
	status, message, checkError := s.lua.CheckHealth(ctx, typeKey, credential)
	if checkError != nil {
		s.stamp(credentialID, string(luaplugin.HealthUnknown))
		if s.logger != nil {
			s.logger.Warn("healthcheck: handler failed, keeping credential",
				"credential_id", credentialID, "type", typeKey, "error", checkError)
		}
		return
	}
	s.stamp(credentialID, string(status))
	if s.logger != nil {
		s.logger.Debug("healthcheck: verdict recorded",
			"credential_id", credentialID, "type", typeKey, "status", string(status))
	}
	if status != luaplugin.HealthUnhealthy {
		return
	}
	reason := message
	if reason == "" {
		reason = "health check reported unhealthy"
	}
	// The provider automation switch masters every non-manual disable:
	// off (or unreadable) stamps the verdict without touching the credential.
	if !s.automationOn(credential) {
		if s.logger != nil {
			s.logger.Debug("healthcheck: automation off, keeping credential",
				"credential_id", credentialID, "type", typeKey)
		}
		return
	}
	if disableError := s.credentialStore.DisableUnhealthy(credentialID, reason); disableError != nil && s.logger != nil {
		s.logger.Warn("healthcheck: disable failed", "credential_id", credentialID, "error", disableError)
	} else if s.logger != nil {
		s.logger.Info("healthcheck: disabled unhealthy credential", "credential_id", credentialID, "type", typeKey)
	}
}

func (s *GormService) stamp(credentialID, status string) {
	_ = s.Stamp(context.Background(), credentialID, status)
}

// toHealthRow maps the service record onto the persistence row. Fields pass
// through verbatim; the credential reference stays a hard foreign key with
// cascade delete.
func toHealthRow(entry *Record) record.CredentialHealth {
	return record.CredentialHealth{
		CredentialID: entry.CredentialID,
		LastCheckAt:  entry.LastCheckAt,
		LastStatus:   entry.LastStatus,
	}
}

// fromHealthRow maps the persistence row back onto the service record.
func fromHealthRow(row record.CredentialHealth) *Record {
	return &Record{
		CredentialID: row.CredentialID,
		LastCheckAt:  row.LastCheckAt,
		LastStatus:   row.LastStatus,
	}
}
