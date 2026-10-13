package credential

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	stdErrors "errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// GormService manages credentials on the SQLite handles with explicit
// converters between wire types and credential rows. Credential rows live in
// the durable database; cooldown parks and health verdicts live in the cache
// file and reference credentials softly. Traditional GORM use: none; every
// statement runs through gorm.G with typed clauses.
type GormService struct {
	database        *db.Database
	providerService *provider.GormService
	logger          *slog.Logger
}

// NewGorm constructs a GORM-backed credential service on the SQLite handle.
// providerService serves backend validation through provider.Resolve; it
// must read non-nil. Construction performs no I/O.
func NewGorm(database *db.Database, providerService *provider.GormService) *GormService {
	return &GormService{database: database, providerService: providerService}
}

// SetLogger wires structured logging.
func (s *GormService) SetLogger(l *slog.Logger) { s.logger = l }

func (s *GormService) log() *slog.Logger {
	if s == nil || s.logger == nil {
		return slog.Default()
	}
	return s.logger
}

// AddOptions holds parameters for adding a new credential to a provider's pool.
type AddOptions struct {
	ProviderID string
	Label      string
	Data       map[string]any
}

// maxParkTTL bounds unified cooldown parks to 90 days, matching storage.
const maxParkTTL = 90 * 24 * time.Hour

// orderCredentialsByIdentifier sorts credential rows by primary key.
func orderCredentialsByIdentifier() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}}}}
}

// requireHandle fails closed when either SQLite handle is missing.
func (s *GormService) requireHandle() (*db.Database, error) {
	if s == nil || s.database == nil || s.database.Handle == nil || s.database.Cache == nil {
		return nil, fmt.Errorf("credential sqlite handle is not configured: %w", apierrors.ErrInvalidInput)
	}
	return s.database, nil
}

// requireProviderService fails closed when backend validation is unwired.
func (s *GormService) requireProviderService() (*provider.GormService, error) {
	if s == nil || s.providerService == nil {
		return nil, fmt.Errorf("credential provider service is not configured: %w", apierrors.ErrInvalidInput)
	}
	return s.providerService, nil
}

// validateCredentialData dispatches credential validation to the Go adapter
// or the Lua validate_credentials handler for the provider type key. Runs
// outside transactions: Lua and adapter calls never run on a transaction
// handle.
func (s *GormService) validateCredentialData(ctx context.Context, providerService *provider.GormService, providerID string, data map[string]any) error {
	resolved, err := provider.Resolve(providerService, ctx, providerID)
	if err != nil {
		return err
	}
	if resolved.IsLua() {
		luaService := providerService.LuaService()
		if luaService == nil {
			return fmt.Errorf("no backend for provider type %q: plugin service not wired", resolved.Instance.TypeKey)
		}
		ok, validateError := luaService.ValidateCredentials(resolved.Instance.TypeKey, data)
		if validateError != nil {
			return fmt.Errorf("invalid credentials: %w", validateError)
		}
		if !ok {
			return fmt.Errorf("invalid credentials: rejected by provider")
		}
		return nil
	}
	if err := resolved.Go.ValidateCredentials(data); err != nil {
		return fmt.Errorf("invalid credentials: %w", err)
	}
	return nil
}

// Add validates and persists a new credential for the given provider.
// Validation runs outside the transaction; the insert runs in one
// transaction on the transaction handle only. The provider row must exist:
// a dangling provider fails through the foreign key as invalid input.
func (s *GormService) Add(ctx context.Context, options AddOptions) (*models.Credential, error) {
	providerService, err := s.requireProviderService()
	if err != nil {
		return nil, err
	}
	if _, err := s.requireHandle(); err != nil {
		return nil, err
	}
	data := options.Data
	if data == nil {
		data = map[string]any{}
	}
	if err := s.validateCredentialData(ctx, providerService, options.ProviderID, data); err != nil {
		s.log().Warn("Credential validation rejects input", "provider_id", options.ProviderID, "error", err)
		return nil, err
	}
	identifier, err := util.GenerateID()
	if err != nil {
		s.log().Error("Credential creation fails", "provider_id", options.ProviderID, "operation", "generate_id", "error", err)
		return nil, err
	}
	now := util.Now()
	credential := &models.Credential{
		ID:         identifier,
		ProviderID: options.ProviderID,
		Label:      options.Label,
		Data:       data,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	row := credentialRecordFromModel(credential)
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if err := gorm.G[record.Credential](transaction).Create(ctx, &row); err != nil {
			return db.Translate(err)
		}
		return nil
	}); err != nil {
		s.log().Error("Credential creation fails", "provider_id", options.ProviderID, "operation", "create", "error", err)
		return nil, err
	}
	s.log().Info("Credential creation succeeds", "cred_id", identifier, "provider_id", options.ProviderID)
	return credentialModelFromRecord(row), nil
}

// Get returns a single credential by identifier. A miss reports the
// not-found sentinel with credential context.
func (s *GormService) Get(ctx context.Context, identifier string) (*models.Credential, error) {
	if _, err := s.requireHandle(); err != nil {
		return nil, err
	}
	row, err := gorm.G[record.Credential](s.database.Handle, clause.Eq{Column: "id", Value: identifier}).First(ctx)
	if err != nil {
		translated := db.Translate(err)
		if stdErrors.Is(translated, apierrors.ErrNotFound) {
			s.log().Debug("Credential lookup misses", "cred_id", identifier)
			return nil, apierrors.NewNotFoundError("credential", identifier)
		}
		s.log().Error("Credential lookup fails", "cred_id", identifier, "operation", "get", "error", translated)
		return nil, translated
	}
	credential := credentialModelFromRecord(row)
	s.log().Debug("Credential lookup succeeds", "cred_id", identifier, "provider_id", credential.ProviderID)
	return credential, nil
}

// ListUsable returns routable credentials for a provider: non-expired and
// not disabled, ordered by identifier. Empty pools yield an empty slice,
// never an error. Selection among them belongs to the plugin.
func (s *GormService) ListUsable(ctx context.Context, providerID string) ([]*models.Credential, error) {
	if _, err := s.requireHandle(); err != nil {
		return nil, err
	}
	rows, err := gorm.G[record.Credential](s.database.Handle, clause.Eq{Column: "provider_id", Value: providerID}, orderCredentialsByIdentifier()).Find(ctx)
	if err != nil {
		translated := db.Translate(err)
		s.log().Error("Credential listing fails", "provider_id", providerID, "operation", "list_usable", "error", translated)
		return nil, translated
	}
	available := make([]*models.Credential, 0, len(rows))
	expired := 0
	disabled := 0
	for _, row := range rows {
		credential := credentialModelFromRecord(row)
		if credential.IsExpired() {
			expired++
			continue
		}
		if credential.Disabled {
			disabled++
			continue
		}
		available = append(available, credential)
	}
	s.log().Debug("Credential listing succeeds", "provider_id", providerID, "count", len(available), "total", len(rows), "expired", expired, "disabled", disabled)
	return available, nil
}

// All returns routable credentials for a provider, denying empty pools:
// the router gate maps absence to NoCredential before plugins run.
func (s *GormService) All(ctx context.Context, providerID string) ([]*models.Credential, error) {
	available, err := s.ListUsable(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if len(available) == 0 {
		s.log().Debug("Credential pool empties", "provider_id", providerID)
		return nil, fmt.Errorf("no available credentials for provider %s: %w", providerID, apierrors.ErrNoCredential)
	}
	return available, nil
}

// ListByProvider returns all credentials for a provider ordered by
// identifier.
func (s *GormService) ListByProvider(ctx context.Context, providerID string) ([]*models.Credential, error) {
	if _, err := s.requireHandle(); err != nil {
		return nil, err
	}
	rows, err := gorm.G[record.Credential](s.database.Handle, clause.Eq{Column: "provider_id", Value: providerID}, orderCredentialsByIdentifier()).Find(ctx)
	if err != nil {
		translated := db.Translate(err)
		s.log().Error("Credential listing fails", "provider_id", providerID, "operation", "list_by_provider", "error", translated)
		return nil, translated
	}
	items := make([]*models.Credential, 0, len(rows))
	for _, row := range rows {
		items = append(items, credentialModelFromRecord(row))
	}
	s.log().Debug("Credential listing succeeds", "provider_id", providerID, "count", len(items))
	return items, nil
}

// ListAll returns every credential across all providers ordered by
// identifier.
func (s *GormService) ListAll(ctx context.Context) ([]*models.Credential, error) {
	if _, err := s.requireHandle(); err != nil {
		return nil, err
	}
	rows, err := gorm.G[record.Credential](s.database.Handle, orderCredentialsByIdentifier()).Find(ctx)
	if err != nil {
		translated := db.Translate(err)
		s.log().Error("Credential listing fails", "operation", "list_all", "error", translated)
		return nil, translated
	}
	items := make([]*models.Credential, 0, len(rows))
	for _, row := range rows {
		items = append(items, credentialModelFromRecord(row))
	}
	s.log().Debug("Credential listing succeeds", "count", len(items))
	return items, nil
}

// Update replaces a credential mutable fields (data, expiry). One
// transaction holds the read-modify-write on the transaction handle only.
func (s *GormService) Update(ctx context.Context, identifier string, data map[string]any, expiresAt *time.Time) error {
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	providerID := ""
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		row, err := gorm.G[record.Credential](transaction, clause.Eq{Column: "id", Value: identifier}).First(ctx)
		if err != nil {
			return db.Translate(err)
		}
		providerID = row.ProviderID
		row.Data = data
		row.ExpiresAt = normalizeCredentialTime(expiresAt)
		row.UpdatedAt = util.Now()
		rows := []record.Credential{row}
		if err := gorm.G[record.Credential](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
			return db.Translate(err)
		}
		return nil
	}); err != nil {
		if stdErrors.Is(err, apierrors.ErrNotFound) {
			s.log().Debug("Credential update misses", "cred_id", identifier)
		} else {
			s.log().Error("Credential update fails", "cred_id", identifier, "operation", "update", "error", err)
		}
		return err
	}
	s.log().Info("Credential update succeeds", "cred_id", identifier, "provider_id", providerID)
	return nil
}

// UpdateDetails edits the admin-facing fields: label, enable/disable, and
// optionally the credential data (revalidated against the provider backend
// when replaced). Validation runs outside the transaction; one transaction
// holds the read-modify-write on the transaction handle only.
func (s *GormService) UpdateDetails(ctx context.Context, identifier string, label *string, disabled *bool, data map[string]any) error {
	providerService, err := s.requireProviderService()
	if err != nil {
		return err
	}
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	if data != nil {
		current, err := s.Get(ctx, identifier)
		if err != nil {
			return err
		}
		if err := s.validateCredentialData(ctx, providerService, current.ProviderID, data); err != nil {
			s.log().Warn("Credential validation rejects input", "cred_id", identifier, "provider_id", current.ProviderID, "error", err)
			return err
		}
	}
	txnProviderID := ""
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		row, err := gorm.G[record.Credential](transaction, clause.Eq{Column: "id", Value: identifier}).First(ctx)
		if err != nil {
			return db.Translate(err)
		}
		txnProviderID = row.ProviderID
		if label != nil {
			row.Label = *label
		}
		if disabled != nil {
			row.Disabled = *disabled
			if *disabled {
				now := util.Now()
				row.DisabledBy = models.DisabledByAdmin
				row.DisabledReason = "disabled by admin"
				row.DisabledAt = &now
			} else {
				row.DisabledBy = ""
				row.DisabledReason = ""
				row.DisabledAt = nil
			}
		}
		if data != nil {
			row.Data = data
		}
		row.UpdatedAt = util.Now()
		rows := []record.Credential{row}
		if err := gorm.G[record.Credential](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
			return db.Translate(err)
		}
		return nil
	}); err != nil {
		if stdErrors.Is(err, apierrors.ErrNotFound) {
			s.log().Debug("Credential update misses", "cred_id", identifier)
		} else {
			s.log().Error("Credential update fails", "cred_id", identifier, "operation", "update_details", "error", err)
		}
		return err
	}
	if disabled != nil {
		if *disabled {
			s.log().Info("Credential disables by admin", "cred_id", identifier, "provider_id", txnProviderID)
		} else {
			s.log().Info("Credential enables by admin", "cred_id", identifier, "provider_id", txnProviderID)
		}
	} else {
		s.log().Info("Credential update succeeds", "cred_id", identifier, "provider_id", txnProviderID)
	}
	return nil
}

// DisableUnhealthy disables a credential on an explicit unhealthy
// health-check verdict. First wins: an already-disabled credential keeps
// its original cause. One transaction holds the read-modify-write on the
// transaction handle only.
func (s *GormService) DisableUnhealthy(ctx context.Context, identifier, reason string) error {
	if reason == "" {
		reason = "health check reported unhealthy"
	}
	if len(reason) > 1024 {
		reason = reason[:1024] + "…[truncated]"
	}
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	changed := false
	noop := false
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		row, err := gorm.G[record.Credential](transaction, clause.Eq{Column: "id", Value: identifier}).First(ctx)
		if err != nil {
			return db.Translate(err)
		}
		if row.Disabled {
			noop = true
			return nil
		}
		now := util.Now()
		row.Disabled = true
		row.DisabledBy = models.DisabledByHealthcheck
		row.DisabledReason = reason
		row.DisabledAt = &now
		row.UpdatedAt = now
		rows := []record.Credential{row}
		if err := gorm.G[record.Credential](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
			return db.Translate(err)
		}
		changed = true
		return nil
	}); err != nil {
		if stdErrors.Is(err, apierrors.ErrNotFound) {
			s.log().Debug("Credential disable misses", "cred_id", identifier, "operation", "disable_unhealthy")
		} else {
			s.log().Error("Credential disable fails", "cred_id", identifier, "operation", "disable_unhealthy", "error", err)
		}
		return err
	}
	if changed {
		s.log().Info("Credential disables by healthcheck", "cred_id", identifier, "reason", reason)
	} else if noop {
		s.log().Debug("Credential disable skips disabled credential", "cred_id", identifier, "operation", "disable_unhealthy")
	}
	return nil
}

// DisableByPlugin disables a credential on a plugin dead-key verdict.
// First wins: an already-disabled credential keeps its original cause, so
// plugin writes never override admin or healthcheck disables. One
// transaction holds the read-modify-write on the transaction handle only.
func (s *GormService) DisableByPlugin(ctx context.Context, identifier, reason string) error {
	if reason == "" {
		reason = "disabled by plugin"
	}
	if len(reason) > 1024 {
		reason = reason[:1024] + "…[truncated]"
	}
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	changed := false
	noop := false
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		row, err := gorm.G[record.Credential](transaction, clause.Eq{Column: "id", Value: identifier}).First(ctx)
		if err != nil {
			return db.Translate(err)
		}
		if row.Disabled {
			noop = true
			return nil
		}
		now := util.Now()
		row.Disabled = true
		row.DisabledBy = models.DisabledByPlugin
		row.DisabledReason = reason
		row.DisabledAt = &now
		row.UpdatedAt = now
		rows := []record.Credential{row}
		if err := gorm.G[record.Credential](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
			return db.Translate(err)
		}
		changed = true
		return nil
	}); err != nil {
		if stdErrors.Is(err, apierrors.ErrNotFound) {
			s.log().Debug("Credential disable misses", "cred_id", identifier, "operation", "disable_by_plugin")
		} else {
			s.log().Error("Credential disable fails", "cred_id", identifier, "operation", "disable_by_plugin", "error", err)
		}
		return err
	}
	if changed {
		s.log().Info("Credential disables by plugin", "cred_id", identifier, "reason", reason)
	} else if noop {
		s.log().Debug("Credential disable skips disabled credential", "cred_id", identifier, "operation", "disable_by_plugin")
	}
	return nil
}

// EnableByPlugin clears a plugin-caused disable when the key proves
// healthy again. Disables owned by admin or healthcheck verdicts stay
// untouched. One transaction holds the read-modify-write on the transaction
// handle only.
func (s *GormService) EnableByPlugin(ctx context.Context, identifier string) error {
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	cleared := false
	noopReason := ""
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		row, err := gorm.G[record.Credential](transaction, clause.Eq{Column: "id", Value: identifier}).First(ctx)
		if err != nil {
			return db.Translate(err)
		}
		if !row.Disabled {
			noopReason = "already_enabled"
			return nil
		}
		if row.DisabledBy != models.DisabledByPlugin {
			noopReason = "foreign_disable"
			return nil
		}
		row.Disabled = false
		row.DisabledBy = ""
		row.DisabledReason = ""
		row.DisabledAt = nil
		row.UpdatedAt = util.Now()
		rows := []record.Credential{row}
		if err := gorm.G[record.Credential](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
			return db.Translate(err)
		}
		cleared = true
		return nil
	}); err != nil {
		if stdErrors.Is(err, apierrors.ErrNotFound) {
			s.log().Debug("Credential enable misses", "cred_id", identifier, "operation", "enable_by_plugin")
		} else {
			s.log().Error("Credential enable fails", "cred_id", identifier, "operation", "enable_by_plugin", "error", err)
		}
		return err
	}
	if cleared {
		s.log().Info("Credential enables by plugin", "cred_id", identifier)
	} else {
		s.log().Debug("Credential enable skips clearing", "cred_id", identifier, "reason", noopReason)
	}
	return nil
}

// Park benches a credential in the shared cooldown store until the ttl
// lapses. Unknown identifiers fail loudly; ttl must read positive and
// bounded. The existence check reads the durable database; the park upsert
// runs in one transaction on the cache handle. The two files share no
// transaction, so the upsert targets the cache handle only.
func (s *GormService) Park(ctx context.Context, identifier string, ttl time.Duration, reason string) error {
	if ttl <= 0 {
		s.log().Warn("Credential park rejects invalid TTL", "cred_id", identifier, "wait", ttl, "reason", "non_positive_ttl")
		return fmt.Errorf("park ttl must be positive")
	}
	if ttl > maxParkTTL {
		s.log().Warn("Credential park rejects invalid TTL", "cred_id", identifier, "wait", ttl, "reason", "ttl_exceeds_limit")
		return fmt.Errorf("park ttl exceeds 90 days")
	}
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	if _, err := s.Get(ctx, identifier); err != nil {
		return err
	}
	now := util.Now()
	entry := &models.ParkEntry{
		CredentialID: identifier,
		Reason:       reason,
		Until:        now.Add(ttl),
		ParkedAt:     now,
	}
	row := parkRecordFromModel(entry)
	if err := s.database.Cache.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if err := gorm.G[record.CredentialPark](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "credential_id"}}, UpdateAll: true}).Create(ctx, &row); err != nil {
			return db.Translate(err)
		}
		return nil
	}); err != nil {
		s.log().Error("Credential park fails", "cred_id", identifier, "operation", "park", "error", err)
		return err
	}
	s.log().Info("Credential park succeeds", "cred_id", identifier, "reason", reason, "until", entry.Until.UTC().Format(time.RFC3339))
	return nil
}

// Unpark clears a cooldown park so the credential serves immediately.
// Missing parks succeed: unparking is idempotent. One transaction holds the
// conditioned delete on the cache handle only.
func (s *GormService) Unpark(ctx context.Context, identifier string) error {
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	var removed int64
	if err := s.database.Cache.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		tracker := gorm.WithResult()
		affected, err := gorm.G[record.CredentialPark](transaction, clause.Eq{Column: "credential_id", Value: identifier}, tracker).Delete(ctx)
		if err != nil {
			return db.Translate(err)
		}
		removed = int64(affected)
		if tracker.RowsAffected > removed {
			removed = tracker.RowsAffected
		}
		return nil
	}); err != nil {
		s.log().Error("Credential unpark fails", "cred_id", identifier, "operation", "unpark", "error", err)
		return err
	}
	if removed == 0 {
		s.log().Debug("Credential unpark misses", "cred_id", identifier)
	}
	s.log().Info("Credential unpark succeeds", "cred_id", identifier, "count", removed)
	return nil
}

// Parked returns the live park entry for a credential, or nil when absent
// or lapsed. Lapsed rows delete on read through the credential_id key. One
// transaction holds the read and the conditional delete on the cache handle
// only.
func (s *GormService) Parked(ctx context.Context, identifier string) (*models.ParkEntry, error) {
	if _, err := s.requireHandle(); err != nil {
		return nil, err
	}
	var result *models.ParkEntry
	expired := false
	if err := s.database.Cache.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		row, err := gorm.G[record.CredentialPark](transaction, clause.Eq{Column: "credential_id", Value: identifier}).First(ctx)
		if err != nil {
			if stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
				return nil
			}
			return db.Translate(err)
		}
		entry := parkModelFromRecord(row)
		if entry.Expired(util.Now()) {
			if _, err := gorm.G[record.CredentialPark](transaction, clause.Eq{Column: "credential_id", Value: identifier}).Delete(ctx); err != nil {
				return db.Translate(err)
			}
			expired = true
			return nil
		}
		result = entry
		return nil
	}); err != nil {
		s.log().Error("Credential park lookup fails", "cred_id", identifier, "operation", "parked", "error", err)
		return nil, err
	}
	if expired {
		s.log().Debug("Credential park expires", "cred_id", identifier)
		return nil, nil
	}
	if result == nil {
		s.log().Debug("Credential park lookup misses", "cred_id", identifier)
		return nil, nil
	}
	s.log().Debug("Credential park lookup succeeds", "cred_id", identifier, "reason", result.Reason)
	return result, nil
}

// PurgeExpiredParks removes every lapsed cooldown park through the Until
// index in one conditioned delete on the cache handle. Returns the removed
// count.
func (s *GormService) PurgeExpiredParks(ctx context.Context) (int64, error) {
	if _, err := s.requireHandle(); err != nil {
		return 0, err
	}
	var removed int64
	if err := s.database.Cache.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		affected, err := gorm.G[record.CredentialPark](transaction, clause.Lte{Column: "until", Value: util.Now()}).Delete(ctx)
		if err != nil {
			return db.Translate(err)
		}
		removed = int64(affected)
		return nil
	}); err != nil {
		s.log().Error("Credential park purge fails", "operation", "purge_expired_parks", "error", err)
		return 0, err
	}
	s.log().Debug("Credential park purge succeeds", "count", removed)
	return removed, nil
}

// Delete removes a credential by identifier in one conditioned delete.
// Cached park and health rows reference the credential softly across files,
// so the delete clears them explicitly after the durable delete succeeds. A
// zero-row delete reports the not-found sentinel through the WithResult
// tracker.
func (s *GormService) Delete(ctx context.Context, identifier string) error {
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		tracker := gorm.WithResult()
		affected, err := gorm.G[record.Credential](transaction, clause.Eq{Column: "id", Value: identifier}, tracker).Delete(ctx)
		if err != nil {
			return db.Translate(err)
		}
		if affected == 0 || tracker.RowsAffected == 0 {
			return fmt.Errorf("credential %q: %w", identifier, apierrors.ErrNotFound)
		}
		return nil
	}); err != nil {
		if stdErrors.Is(err, apierrors.ErrNotFound) {
			s.log().Debug("Credential deletion misses", "cred_id", identifier)
		} else {
			s.log().Error("Credential deletion fails", "cred_id", identifier, "operation", "delete", "error", err)
		}
		return err
	}
	s.clearCachedCredentialRows(ctx, []string{identifier})
	s.log().Info("Credential deletion succeeds", "cred_id", identifier, "count", 1)
	return nil
}

// DeleteByProvider removes every credential of one provider in one
// conditioned delete. Cached park and health rows clear explicitly after the
// durable delete succeeds. Returns the removed count.
func (s *GormService) DeleteByProvider(ctx context.Context, providerID string) (int, error) {
	if _, err := s.requireHandle(); err != nil {
		return 0, err
	}
	var removed int64
	var credentialIDs []string
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		rows, err := gorm.G[record.Credential](transaction, clause.Eq{Column: "provider_id", Value: providerID}).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		for _, row := range rows {
			credentialIDs = append(credentialIDs, row.ID)
		}
		tracker := gorm.WithResult()
		affected, err := gorm.G[record.Credential](transaction, clause.Eq{Column: "provider_id", Value: providerID}, tracker).Delete(ctx)
		if err != nil {
			return db.Translate(err)
		}
		_ = affected
		removed = tracker.RowsAffected
		return nil
	}); err != nil {
		s.log().Error("Credential provider deletion fails", "provider_id", providerID, "operation", "delete_by_provider", "error", err)
		return 0, err
	}
	s.clearCachedCredentialRows(ctx, credentialIDs)
	s.log().Info("Credential provider deletion succeeds", "provider_id", providerID, "count", removed)
	return int(removed), nil
}

// clearCachedCredentialRows removes cached park and health rows for the
// given credentials. The durable delete already succeeded, so failures log
// and continue: orphaned cache rows never match a credential lookup and the
// next delete or prune pass converges them.
func (s *GormService) clearCachedCredentialRows(ctx context.Context, credentialIDs []string) {
	if len(credentialIDs) == 0 {
		return
	}
	values := make([]any, 0, len(credentialIDs))
	for _, identifier := range credentialIDs {
		values = append(values, identifier)
	}
	condition := clause.IN{Column: "credential_id", Values: values}
	if _, err := gorm.G[record.CredentialPark](s.database.Cache, condition).Delete(ctx); err != nil {
		s.log().Warn("Credential cache park cleanup fails", "count", len(credentialIDs), "error", err)
	}
	if _, err := gorm.G[record.CredentialHealth](s.database.Cache, condition).Delete(ctx); err != nil {
		s.log().Warn("Credential cache health cleanup fails", "count", len(credentialIDs), "error", err)
	}
}
