// GORM-backed database inspection and repair.
//
// GormService mirrors Service on the SQLite handle. The bbolt Service stays
// live until cutover; this type owns the GORM path. Reads run through the
// narrow inventory interfaces below so repairs stay inside the owning
// services: virtual duplicates delete through GormVirtualInventory,
// missing backends disable through GormProviderInventory, dangling token
// models rewrite through GormTokenInventory. No bbolt bucket is touched
// here, and no direct write runs outside those interfaces.
//
// Foreign keys prevent five bbolt orphan categories, so the GORM report
// never emits them: credentials, model overrides, model snapshots, managed
// virtual models, and plugin storage rows all cascade from their provider
// or plugin parents (record/providers.go, record/catalog.go,
// record/virtual.go, record/plugins.go). The surviving checks are the
// duplicate scan (name_key UNIQUE), the corrupt-value scan (no writer
// builds unparseable values), the missing-backend lookup (plugin and Go
// registries, no foreign key), the token soft-reference scan
// (token_allowed_models carries no foreign key by design), and the SQLite
// integrity check through the maintenance.go allowlist.
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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
// provider.Service, which owns the adapter registry on both paths.
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
	_ GoBackendChecker      = (*provider.Service)(nil)
	_ PluginBackendChecker  = (*luaplugin.Service)(nil)
)

// GormService inspects SQLite state through the inventory interfaces
// plus direct reads of the plugin_storage table (luaplugin exposes no
// storage list) and the allowlisted integrity check.
type GormService struct {
	sqlite         *db.Database
	providers      GormProviderInventory
	virtualModels  GormVirtualInventory
	tokens         GormTokenInventory
	goBackends     GoBackendChecker
	pluginBackends PluginBackendChecker
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

// orderPluginStorageByKeys sorts storage rows by composite key.
func orderPluginStorageByKeys() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{
		{Column: clause.Column{Name: "plugin_id"}},
		{Column: clause.Column{Name: "scope"}},
		{Column: clause.Column{Name: "key"}},
	}}
}

// joinStorageKey renders a storage row key in the bbolt NUL-joined
// display form.
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
	if s == nil || s.sqlite == nil || s.sqlite.Handle == nil {
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
		return nil, err
	}

	providerList, err := s.providers.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	validProviders := make(map[string]bool, len(providerList))
	for _, providerInstance := range providerList {
		if providerInstance != nil {
			validProviders[providerInstance.ID] = true
		}
	}

	var issues []Issue

	// Providers without backend mirror the bbolt check: disabled
	// providers skip, Go adapters satisfy first, then the plugin
	// registry. A nil checker skips its leg.
	var missingBackendKeys []string
	for _, providerInstance := range providerList {
		if providerInstance == nil || providerInstance.Disabled {
			continue
		}
		if s.goBackends != nil {
			if _, ok := s.goBackends.GoAdapterFor(providerInstance.TypeKey); ok {
				continue
			}
		}
		if s.pluginBackends == nil {
			continue
		}
		if _, err := s.pluginBackends.Lookup(providerInstance.TypeKey); err == nil {
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
	// rows land outside the service.
	virtualList, err := s.virtualModels.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list virtual models: %w", err)
	}
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
	// reader contract and stay unflagged. Inspect-only, never
	// auto-cleaned.
	storageRows, err := gorm.G[record.PluginStorage](s.sqlite.Handle, orderPluginStorageByKeys()).Find(ctx)
	if err != nil {
		return nil, fmt.Errorf("list plugin storage: %w", db.Translate(err))
	}
	var corruptStorageKeys []string
	for _, row := range storageRows {
		if len(row.Value) == 0 {
			continue
		}
		var probe any
		if err := json.Unmarshal(row.Value, &probe); err != nil {
			corruptStorageKeys = append(corruptStorageKeys, joinStorageKey(row.PluginID, row.Scope, row.Key))
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

	// Integrity check runs through the allowlisted PRAGMA. A failure
	// is data, so it lands as an issue instead of an error.
	if err := db.CheckIntegrity(ctx, s.sqlite.Handle); err != nil {
		issues = append(issues, Issue{
			Category:    CategoryIntegrityCheck,
			Title:       "Database Integrity Check Failed",
			Description: fmt.Sprintf("SQLite integrity check failed: %v. Manual database review required, never auto-cleaned.", err),
			Count:       1,
			Keys:        []string{"integrity_check"},
		})
	}

	total := 0
	for _, issue := range issues {
		total += issue.Count
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

	for _, issue := range report.Issues {
		if !allCategories && !categoryMap[issue.Category] {
			continue
		}

		switch issue.Category {
		case CategoryMissingProviderBackend:
			for _, key := range issue.Keys {
				instance, err := s.providers.Get(ctx, key)
				if err != nil || instance == nil {
					continue
				}
				if err := s.providers.SystemDisable(ctx, key, "doctor: no installed plugin or built-in backend for type "+instance.TypeKey); err == nil {
					totalFixed++
				}
			}

		case CategoryDuplicateVirtualModels:
			virtualList, err := s.virtualModels.List(ctx)
			if err != nil {
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
					if err := s.virtualModels.Delete(ctx, identifier); err == nil {
						totalFixed++
					}
				}
			}

		case CategoryBrokenTokenIndexes:
			danglingByToken := map[string]map[string]bool{}
			for _, key := range issue.Keys {
				tokenID, modelID, ok := splitTokenModelKey(key)
				if !ok {
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
				if err := s.tokens.UpdateRules(ctx, tokenID, rules); err == nil {
					totalFixed += removed
				}
			}

		case CategoryCorruptPluginStorage, CategoryIntegrityCheck,
			CategoryOrphanCredentials, CategoryOrphanModelOverrides,
			CategoryOrphanModelInfos, CategoryOrphanVirtualModels,
			CategoryOrphanPluginStorage:
			// Inspect-only (corrupt storage, integrity) or retired on
			// this path (foreign keys prevent every orphan): no repair.
		}
	}

	return totalFixed, nil
}
