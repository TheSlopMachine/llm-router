// GORM-backed database inspection and repair.
//
// GormService inspects both SQLite files. Reads run through the
// narrow inventory interfaces below so repairs stay inside the owning
// services: virtual duplicates delete through GormVirtualInventory,
// missing backends disable through GormProviderInventory, dangling token
// models rewrite through GormTokenInventory. No direct write runs outside
// those interfaces.
//
// Foreign keys plus explicit service cleanup prevent the legacy orphan
// categories, so the GORM report never emits them: credentials, model
// overrides, managed virtual models, and durable plugin storage rows
// cascade from their provider or plugin parents (record/providers.go,
// record/catalog.go, record/virtual.go, record/plugins.go), while cached
// snapshots, parks, health verdicts, and TTL storage rows clear explicitly
// through their owning service deletes. The surviving checks are the
// duplicate scan (name_key UNIQUE), the corrupt-value scan (no writer
// builds unparseable values, scanned in both files), the missing-backend
// lookup (plugin and Go registries, no foreign key), the token
// soft-reference scan (token_allowed_models carries no foreign key by
// design), and the SQLite integrity check on both files through the
// maintenance.go allowlist.
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	stdErrors "errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
)

// IssueCategory names one doctor inspection category.
type IssueCategory string

const (
	CategoryDuplicateVirtualModels IssueCategory = "duplicate_virtual_models"
	CategoryCorruptPluginStorage   IssueCategory = "corrupt_plugin_storage"
	CategoryMissingProviderBackend IssueCategory = "missing_provider_backend"
	CategoryBrokenTokenIndexes     IssueCategory = "broken_token_indexes"
)

// Issue is one inspection finding with the affected keys.
type Issue struct {
	Category    IssueCategory `json:"category"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Count       int           `json:"count"`
	Keys        []string      `json:"keys,omitempty"`
}

// InspectionReport is the full inspection result.
type InspectionReport struct {
	Issues      []Issue `json:"issues"`
	TotalIssues int     `json:"total_issues"`
}

// CategoryIntegrityCheck reports a failed SQLite integrity check. The
// failure is data, so Inspect surfaces it as an issue instead of an
// error; repair stays manual.
const CategoryIntegrityCheck IssueCategory = "integrity_check"

// GormProviderInventory lists providers and disables them. Satisfied by
// provider.GormService; repairs never touch provider rows directly.
type GormProviderInventory interface {
	List(ctx context.Context) ([]*models.ProviderInstance, error)
	Get(ctx context.Context, identifier string) (*models.ProviderInstance, error)
	SystemDisable(ctx context.Context, identifier, reason string) error
}

// GormVirtualInventory lists virtual models and deletes them. Satisfied
// by virtual.GormService; duplicate repair deletes through it.
type GormVirtualInventory interface {
	List(ctx context.Context) ([]*models.VirtualModel, error)
	Delete(ctx context.Context, identifier string) error
}

// GormTokenInventory lists tokens and rewrites their rules. Satisfied by
// token.GormService; dangling-model repair filters through UpdateRules
// because no single-row delete exists on the join tables.
type GormTokenInventory interface {
	List(ctx context.Context) ([]*models.RouterToken, error)
	Get(ctx context.Context, identifier string) (*models.RouterToken, error)
	UpdateRules(ctx context.Context, identifier string, rules models.TokenRules) error
}

// GoBackendChecker reports built-in Go backends. Satisfied by
// provider.GormService, which owns the adapter registry.
type GoBackendChecker interface {
	GoAdapterFor(typeKey string) (provider.GoAdapter, bool)
}

// PluginBackendChecker reports installed plugin backends. Satisfied by
// luaplugin.Service; the registry is storage-agnostic.
type PluginBackendChecker interface {
	Lookup(typeKey string) (*luaplugin.PluginRecord, error)
}

// Compile-time proof that the GORM-backed services and the registry
// owners plug into the narrow doctor interfaces.
var (
	_ GormProviderInventory = (*provider.GormService)(nil)
	_ GormVirtualInventory  = (*virtual.GormService)(nil)
	_ GormTokenInventory    = (*token.GormService)(nil)
	_ GoBackendChecker      = (*provider.GormService)(nil)
	_ PluginBackendChecker  = (*luaplugin.Service)(nil)
)

// GormService inspects SQLite state through the inventory interfaces
// plus direct reads of the plugin_storage tables in both files (luaplugin
// exposes no storage list) and the allowlisted integrity check.
type GormService struct {
	sqlite         *db.Database
	providers      GormProviderInventory
	virtualModels  GormVirtualInventory
	tokens         GormTokenInventory
	goBackends     GoBackendChecker
	pluginBackends PluginBackendChecker
	logger         *slog.Logger
}

// NewGorm constructs a GORM-backed doctor service. providers,
// virtualModels, and tokens are required; goBackends and pluginBackends
// may be nil, in which case the missing-backend legs they own stay
// skipped. Construction performs no I/O.
func NewGorm(
	sqlite *db.Database,
	providers GormProviderInventory,
	virtualModels GormVirtualInventory,
	tokens GormTokenInventory,
	goBackends GoBackendChecker,
	pluginBackends PluginBackendChecker,
) *GormService {
	return &GormService{
		sqlite:         sqlite,
		providers:      providers,
		virtualModels:  virtualModels,
		tokens:         tokens,
		goBackends:     goBackends,
		pluginBackends: pluginBackends,
	}
}

// SetLogger wires structured logging.
func (s *GormService) SetLogger(logger *slog.Logger) {
	if s == nil {
		return
	}
	s.logger = logger
}

// orderPluginStorageByKeys sorts storage rows by composite key.
func orderPluginStorageByKeys() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{
		{Column: clause.Column{Name: "plugin_id"}},
		{Column: clause.Column{Name: "scope"}},
		{Column: clause.Column{Name: "key"}},
	}}
}

// joinStorageKey renders a storage row key in the NUL-joined display form.
func joinStorageKey(pluginID, scope, key string) string {
	return pluginID + "\x00" + scope + "\x00" + key
}

// joinTokenModelKey renders a dangling allow-list entry as
// tokenID NUL modelID.
func joinTokenModelKey(tokenID, modelID string) string {
	return tokenID + "\x00" + modelID
}

// splitTokenModelKey inverts joinTokenModelKey.
func splitTokenModelKey(raw string) (tokenID, modelID string, ok bool) {
	parts := strings.SplitN(raw, "\x00", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// requireInventories fails closed when mandatory dependencies are missing.
func (s *GormService) requireInventories() error {
	if s == nil || s.sqlite == nil || s.sqlite.Handle == nil || s.sqlite.Cache == nil {
		return fmt.Errorf("doctor sqlite handle is not configured: %w", apierrors.ErrInvalidInput)
	}
	if s.providers == nil {
		return fmt.Errorf("doctor provider inventory is not configured: %w", apierrors.ErrInvalidInput)
	}
	if s.virtualModels == nil {
		return fmt.Errorf("doctor virtual inventory is not configured: %w", apierrors.ErrInvalidInput)
	}
	if s.tokens == nil {
		return fmt.Errorf("doctor token inventory is not configured: %w", apierrors.ErrInvalidInput)
	}
	return nil
}

// Inspect scans SQLite state for duplicate, corrupt, backend, and
// soft-reference issues plus the integrity check. Every inventory
// failure returns an error; unknown state never reads clean.
func (s *GormService) Inspect(ctx context.Context) (*InspectionReport, error) {
	if err := s.requireInventories(); err != nil {
		if s != nil && s.logger != nil {
			s.logger.Error("Inspection aborted", "subsystem", "doctor", "reason", "missing_inventory", "error", err)
		}
		return nil, err
	}

	providerList, err := s.providers.List(ctx)
	if err != nil {
		if s != nil && s.logger != nil {
			s.logger.Error("Inspection aborted", "subsystem", "providers", "reason", "list_failed", "error", err)
		}
		return nil, fmt.Errorf("list providers: %w", err)
	}
	validProviders := make(map[string]bool, len(providerList))
	for _, providerInstance := range providerList {
		if providerInstance != nil {
			validProviders[providerInstance.ID] = true
		}
	}

	var issues []Issue

	// Providers without backend mirror the previous check: Go adapters
	// satisfy first, then the plugin registry. A nil checker skips its
	// leg. Disabled providers skip unless they still hold managed virtual
	// rows: a disable without cleanup keeps listing routable-looking
	// models, so the issue stays until the rows are gone.
	var missingBackendKeys []string
	virtualList, err := s.virtualModels.List(ctx)
	if err != nil {
		if s != nil && s.logger != nil {
			s.logger.Error("Inspection aborted", "subsystem", "virtual_models", "reason", "list_failed", "error", err)
		}
		return nil, fmt.Errorf("list virtual models: %w", err)
	}
	skippedCount := 0
	managedByProvider := map[string]bool{}
	for _, virtualModel := range virtualList {
		if virtualModel == nil {
			continue
		}
		markerProvider, _, markerError := virtual.ParseMarker(virtualModel.ManagedBy)
		if markerError != nil {
			skippedCount++
			if s != nil && s.logger != nil {
				s.logger.Warn("Virtual model marker parse failed, skipping entry", "subsystem", "virtual_models", "reason", "corrupt_marker", "error", markerError)
			}
			continue
		}
		managedByProvider[markerProvider] = true
	}
	if s != nil && s.goBackends == nil && s.logger != nil {
		s.logger.Warn("Backend checker skipped", "subsystem", "providers", "reason", "nil_go_checker")
	}
	if s != nil && s.pluginBackends == nil && s.logger != nil {
		s.logger.Warn("Backend checker skipped", "subsystem", "providers", "reason", "nil_plugin_checker")
	}
	for _, providerInstance := range providerList {
		if providerInstance == nil {
			continue
		}
		if s.goBackends != nil {
			if _, ok := s.goBackends.GoAdapterFor(providerInstance.TypeKey); ok {
				continue
			}
		}
		if s.pluginBackends == nil {
			skippedCount++
			continue
		}
		if _, err := s.pluginBackends.Lookup(providerInstance.TypeKey); err == nil {
			continue
		} else if stdErrors.Is(err, luaplugin.ErrPluginDisabled) {
			continue
		}
		if providerInstance.Disabled && !managedByProvider[providerInstance.ID] {
			continue
		}
		missingBackendKeys = append(missingBackendKeys, providerInstance.ID)
	}
	if len(missingBackendKeys) > 0 {
		sort.Strings(missingBackendKeys)
		issues = append(issues, Issue{
			Category:    CategoryMissingProviderBackend,
			Title:       "Providers Without Backend",
			Description: "Providers whose type has no installed plugin or built-in backend: requests fail and credentials never refresh. Install the missing plugin or clean to disable; re-enable after installing.",
			Count:       len(missingBackendKeys),
			Keys:        missingBackendKeys,
		})
	}

	// Duplicate virtual models group by lowercased name. The name_key
	// UNIQUE index rejects these on write, so this stays empty unless
	// rows land outside the service. The list above feeds both scans.
	nameCounts := map[string]int{}
	nameToIdentifiers := map[string][]string{}
	for _, virtualModel := range virtualList {
		if virtualModel == nil {
			continue
		}
		lowerName := strings.ToLower(virtualModel.Name)
		nameCounts[lowerName]++
		nameToIdentifiers[lowerName] = append(nameToIdentifiers[lowerName], virtualModel.ID)
	}
	var duplicateKeys []string
	for name, count := range nameCounts {
		if count > 1 {
			duplicateKeys = append(duplicateKeys, nameToIdentifiers[name]...)
		}
	}
	if len(duplicateKeys) > 0 {
		sort.Strings(duplicateKeys)
		issues = append(issues, Issue{
			Category:    CategoryDuplicateVirtualModels,
			Title:       "Duplicate Virtual Models",
			Description: "Multiple virtual models sharing the same name",
			Count:       len(duplicateKeys),
			Keys:        duplicateKeys,
		})
	}

	// Corrupt plugin storage flags values no writer builds: the GORM
	// writer always stores JSON, so unmarshal failures prove manual or
	// foreign writes. Empty values read as missing per the storage
	// reader contract and stay unflagged. Both files scan: durable rows
	// live in the database, TTL rows in the cache. Inspect-only, never
	// auto-cleaned.
	corruptStorageKeys := []string{}
	corruptSeen := map[string]bool{}
	for _, handle := range []*gorm.DB{s.sqlite.Handle, s.sqlite.Cache} {
		storageRows, err := gorm.G[record.PluginStorage](handle, orderPluginStorageByKeys()).Find(ctx)
		if err != nil {
			if s != nil && s.logger != nil {
				s.logger.Error("Inspection aborted", "subsystem", "plugin_storage", "reason", "list_failed", "error", err)
			}
			return nil, fmt.Errorf("list plugin storage: %w", db.Translate(err))
		}
		for _, row := range storageRows {
			if len(row.Value) == 0 {
				continue
			}
			var probe any
			if err := json.Unmarshal(row.Value, &probe); err != nil {
				key := joinStorageKey(row.PluginID, row.Scope, row.Key)
				if !corruptSeen[key] {
					corruptSeen[key] = true
					corruptStorageKeys = append(corruptStorageKeys, key)
				}
			}
		}
	}
	if len(corruptStorageKeys) > 0 {
		sort.Strings(corruptStorageKeys)
		issues = append(issues, Issue{
			Category:    CategoryCorruptPluginStorage,
			Title:       "Corrupt Plugin Storage",
			Description: "Stored rows no writer builds: manual database review required, never auto-cleaned",
			Count:       len(corruptStorageKeys),
			Keys:        corruptStorageKeys,
		})
	}

	// Dangling token models flag allow-list entries whose provider
	// prefix has no provider row. Allowed models are the only token
	// join without a foreign key, so provider deletes leave them
	// behind while every other allow-list row cascades.
	tokenList, err := s.tokens.List(ctx)
	if err != nil {
		if s != nil && s.logger != nil {
			s.logger.Error("Inspection aborted", "subsystem", "tokens", "reason", "list_failed", "error", err)
		}
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	var danglingModelKeys []string
	for _, tokenRecord := range tokenList {
		if tokenRecord == nil {
			continue
		}
		for _, allowedModel := range tokenRecord.Rules.AllowedModels {
			providerID, _, parseError := allowedModel.Parse()
			if parseError != nil || !validProviders[providerID] {
				danglingModelKeys = append(danglingModelKeys, joinTokenModelKey(tokenRecord.ID, string(allowedModel)))
			}
		}
	}
	if len(danglingModelKeys) > 0 {
		sort.Strings(danglingModelKeys)
		issues = append(issues, Issue{
			Category:    CategoryBrokenTokenIndexes,
			Title:       "Dangling Token Model Entries",
			Description: "Token allow-list models referencing missing providers: authorization checks deny them. Clean to drop the dangling entries.",
			Count:       len(danglingModelKeys),
			Keys:        danglingModelKeys,
		})
	}

	// Integrity checks run through the allowlisted PRAGMA on both files. A
	// failure is data, so it lands as an issue instead of an error.
	for _, target := range []struct {
		name   string
		key    string
		handle *gorm.DB
	}{
		{"database", "integrity_check", s.sqlite.Handle},
		{"cache", "integrity_check_cache", s.sqlite.Cache},
	} {
		if err := db.CheckIntegrity(ctx, target.handle); err != nil {
			issues = append(issues, Issue{
				Category:    CategoryIntegrityCheck,
				Title:       "Database Integrity Check Failed",
				Description: fmt.Sprintf("SQLite %s integrity check failed: %v. Manual database review required, never auto-cleaned.", target.name, err),
				Count:       1,
				Keys:        []string{target.key},
			})
		}
	}

	total := 0
	for _, issue := range issues {
		total += issue.Count
	}

	if s != nil && s.logger != nil {
		s.logger.Info("Inspection completed", "subsystem", "doctor", "count", total, "examined", len(providerList)+len(virtualList)+len(tokenList), "skipped", skippedCount, "failed", 0)
		for _, issue := range issues {
			s.logger.Info("Inspection category completed", "subsystem", string(issue.Category), "count", issue.Count)
		}
	}

	return &InspectionReport{
		Issues:      issues,
		TotalIssues: total,
	}, nil
}

// Fix resolves issues for the given categories, or all fixable categories
// when empty. Duplicate virtual models keep the lowest identifier per
// name group; missing backends disable through the provider inventory;
// dangling token models drop through rule rewrites. Corrupt storage and
// integrity failures are inspect-only, and the retired orphan categories
// never occur on this path, so all three stay untouched.
func (s *GormService) Fix(ctx context.Context, categories []IssueCategory) (int, error) {
	report, err := s.Inspect(ctx)
	if err != nil {
		return 0, err
	}

	categoryMap := map[IssueCategory]bool{}
	for _, category := range categories {
		categoryMap[category] = true
	}
	allCategories := len(categories) == 0

	totalFixed := 0
	failedCount := 0
	skippedCount := 0
	fixedByCategory := map[IssueCategory]int{}

	for _, issue := range report.Issues {
		if !allCategories && !categoryMap[issue.Category] {
			continue
		}

		switch issue.Category {
		case CategoryMissingProviderBackend:
			for _, key := range issue.Keys {
				instance, err := s.providers.Get(ctx, key)
				if err != nil || instance == nil {
					skippedCount++
					if s != nil && s.logger != nil {
						s.logger.Warn("Fix skipped missing provider", "subsystem", string(issue.Category), "provider_id", key, "reason", "not_found")
					}
					continue
				}
				if err := s.providers.SystemDisable(ctx, key, "doctor: no installed plugin or built-in backend for type "+instance.TypeKey); err != nil {
					failedCount++
					if s != nil && s.logger != nil {
						s.logger.Error("Fix disable failed", "subsystem", string(issue.Category), "provider_id", key, "completed_so_far", totalFixed, "error", err)
					}
					continue
				}
				totalFixed++
				fixedByCategory[issue.Category]++
				// A disabled provider holds no managed virtual models:
				// drop its rows so they stop listing and routing.
				virtualList, err := s.virtualModels.List(ctx)
				if err != nil {
					skippedCount++
					if s != nil && s.logger != nil {
						s.logger.Warn("Fix skipped virtual cleanup", "subsystem", string(issue.Category), "provider_id", key, "reason", "list_failed")
					}
					continue
				}
				for _, virtualModel := range virtualList {
					if virtualModel == nil {
						continue
					}
					markerProvider, _, markerError := virtual.ParseMarker(virtualModel.ManagedBy)
					if markerError != nil {
						skippedCount++
						if s != nil && s.logger != nil {
							s.logger.Warn("Virtual model marker parse failed, skipping entry", "subsystem", string(issue.Category), "reason", "corrupt_marker", "error", markerError)
						}
						continue
					}
					if markerProvider != key {
						continue
					}
					if err := s.virtualModels.Delete(ctx, virtualModel.ID); err != nil {
						failedCount++
						if s != nil && s.logger != nil {
							s.logger.Error("Fix delete failed", "subsystem", string(issue.Category), "provider_id", key, "completed_so_far", totalFixed, "error", err)
						}
					} else {
						totalFixed++
						fixedByCategory[issue.Category]++
					}
				}
			}

		case CategoryDuplicateVirtualModels:
			virtualList, err := s.virtualModels.List(ctx)
			if err != nil {
				if s != nil && s.logger != nil {
					s.logger.Error("Fix aborted", "subsystem", string(issue.Category), "completed_so_far", totalFixed, "error", err)
				}
				return totalFixed, fmt.Errorf("list virtual models for fix: %w", err)
			}
			identifiersByName := map[string][]string{}
			for _, virtualModel := range virtualList {
				if virtualModel == nil {
					continue
				}
				lowerName := strings.ToLower(virtualModel.Name)
				identifiersByName[lowerName] = append(identifiersByName[lowerName], virtualModel.ID)
			}
			for _, identifiers := range identifiersByName {
				if len(identifiers) < 2 {
					continue
				}
				sort.Strings(identifiers)
				for _, identifier := range identifiers[1:] {
					if err := s.virtualModels.Delete(ctx, identifier); err != nil {
						failedCount++
						if s != nil && s.logger != nil {
							s.logger.Error("Fix delete failed", "subsystem", string(issue.Category), "completed_so_far", totalFixed, "error", err)
						}
					} else {
						totalFixed++
						fixedByCategory[issue.Category]++
					}
				}
			}

		case CategoryBrokenTokenIndexes:
			danglingByToken := map[string]map[string]bool{}
			for _, key := range issue.Keys {
				tokenID, modelID, ok := splitTokenModelKey(key)
				if !ok {
					skippedCount++
					if s != nil && s.logger != nil {
						s.logger.Warn("Token model key parse failed, skipping entry", "subsystem", string(issue.Category), "reason", "split_failed")
					}
					continue
				}
				if danglingByToken[tokenID] == nil {
					danglingByToken[tokenID] = map[string]bool{}
				}
				danglingByToken[tokenID][modelID] = true
			}
			tokenIDs := make([]string, 0, len(danglingByToken))
			for tokenID := range danglingByToken {
				tokenIDs = append(tokenIDs, tokenID)
			}
			sort.Strings(tokenIDs)
			for _, tokenID := range tokenIDs {
				dangling := danglingByToken[tokenID]
				stored, err := s.tokens.Get(ctx, tokenID)
				if err != nil || stored == nil {
					skippedCount++
					if s != nil && s.logger != nil {
						s.logger.Warn("Fix skipped missing token", "subsystem", string(issue.Category), "reason", "not_found")
					}
					continue
				}
				kept := make([]models.ModelId, 0, len(stored.Rules.AllowedModels))
				removed := 0
				for _, allowedModel := range stored.Rules.AllowedModels {
					if dangling[string(allowedModel)] {
						removed++
						continue
					}
					kept = append(kept, allowedModel)
				}
				if removed == 0 {
					continue
				}
				rules := stored.Rules
				rules.AllowedModels = kept
				if err := s.tokens.UpdateRules(ctx, tokenID, rules); err != nil {
					failedCount++
					if s != nil && s.logger != nil {
						s.logger.Error("Fix update rules failed", "subsystem", string(issue.Category), "completed_so_far", totalFixed, "error", err)
					}
				} else {
					totalFixed += removed
					fixedByCategory[issue.Category] += removed
				}
			}

		case CategoryCorruptPluginStorage, CategoryIntegrityCheck:
			// Inspect-only: corrupt storage values have no writer-side
			// repair and integrity failures stay manual.
			skippedCount += len(issue.Keys)
		}
	}

	if s != nil && s.logger != nil {
		s.logger.Info("Fix completed", "subsystem", "doctor", "fixed", totalFixed, "examined", len(report.Issues), "skipped", skippedCount, "failed", failedCount)
		for category, fixed := range fixedByCategory {
			s.logger.Info("Fix category completed", "subsystem", string(category), "fixed", fixed)
		}
	}

	return totalFixed, nil
}
