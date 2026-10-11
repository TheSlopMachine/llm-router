package doctor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
	bolt "go.etcd.io/bbolt"
)

type IssueCategory string

const (
	CategoryOrphanCredentials      IssueCategory = "orphan_credentials"
	CategoryOrphanModelOverrides   IssueCategory = "orphan_model_overrides"
	CategoryOrphanModelInfos       IssueCategory = "orphan_model_infos"
	CategoryOrphanVirtualModels    IssueCategory = "orphan_virtual_models"
	CategoryDuplicateVirtualModels IssueCategory = "duplicate_virtual_models"
	CategoryOrphanPluginStorage    IssueCategory = "orphan_plugin_storage"
	CategoryCorruptPluginStorage   IssueCategory = "corrupt_plugin_storage"
	CategoryMissingProviderBackend IssueCategory = "missing_provider_backend"
	CategoryBrokenTokenIndexes     IssueCategory = "broken_token_indexes"
)

type Issue struct {
	Category    IssueCategory `json:"category"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Count       int           `json:"count"`
	Keys        []string      `json:"keys,omitempty"`
}

type InspectionReport struct {
	Issues      []Issue `json:"issues"`
	TotalIssues int     `json:"total_issues"`
}

type Service struct {
	database          *db.DB
	providerService   *provider.Service
	credentialService *credential.Service
	virtualService    *virtual.Service
	tokenService      *token.Service
	proxyService      *proxypool.Service
	luaService        *luaplugin.Service
	modelInfoService  *modelinfo.Service
}

func New(
	database *db.DB,
	providerService *provider.Service,
	credentialService *credential.Service,
	virtualService *virtual.Service,
	tokenService *token.Service,
	proxyService *proxypool.Service,
	luaService *luaplugin.Service,
	modelInfoService *modelinfo.Service,
) *Service {
	return &Service{
		database:          database,
		providerService:   providerService,
		credentialService: credentialService,
		virtualService:    virtualService,
		tokenService:      tokenService,
		proxyService:      proxyService,
		luaService:        luaService,
		modelInfoService:  modelInfoService,
	}
}

// Inspect scans the database for orphaned or corrupt data.
func (s *Service) Inspect() (*InspectionReport, error) {
	var issues []Issue

	// Build lookup maps of valid entities
	providers, err := s.providerService.List()
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	validProviders := map[string]bool{}
	validProviderTypes := map[string]bool{}
	for _, providerInstance := range providers {
		validProviders[providerInstance.ID] = true
		validProviderTypes[providerInstance.TypeKey] = true
	}

	validProxies := map[string]bool{}
	if s.proxyService != nil {
		proxyIDs, err := s.proxyService.KnownIDs()
		if err != nil {
			return nil, fmt.Errorf("list cached proxies: %w", err)
		}
		for _, id := range proxyIDs {
			validProxies[id] = true
		}
	}

	validPlugins := map[string]bool{}
	if s.luaService != nil {
		if plugins, err := s.luaService.List(); err == nil {
			for _, plugin := range plugins {
				validPlugins[plugin.ID] = true
			}
		}
	}

	// 1. Orphan Credentials
	if credentials, err := s.credentialService.ListAll(); err == nil {
		var keys []string
		for _, credentialItem := range credentials {
			if !validProviders[credentialItem.ProviderID] {
				keys = append(keys, credentialItem.ID)
			}
		}
		if len(keys) > 0 {
			issues = append(issues, Issue{
				Category:    CategoryOrphanCredentials,
				Title:       "Orphan Credentials",
				Description: "API keys pointing to deleted or non-existent providers",
				Count:       len(keys),
				Keys:        keys,
			})
		}
	}

	// Providers Without Backend. A provider whose type has no installed
	// plugin or built-in Go backend fails every request, discovery, and
	// refresh with a lookup error. The backend test mirrors
	// provider.Resolve: Go adapter first, then the plugin registry.
	// Disabled providers skip: a parked provider is intentional, not sick.
	// Fix disables the provider (first-wins, manual re-enable clears);
	// installing the missing plugin then re-enabling restores it.
	var missingBackendKeys []string
	for _, providerInstance := range providers {
		if providerInstance.Disabled {
			continue
		}
		if _, ok := s.providerService.GoAdapterFor(providerInstance.TypeKey); ok {
			continue
		}
		if s.luaService == nil {
			continue
		}
		if _, err := s.luaService.Lookup(providerInstance.TypeKey); err == nil {
			continue
		}
		missingBackendKeys = append(missingBackendKeys, providerInstance.ID)
	}
	if len(missingBackendKeys) > 0 {
		issues = append(issues, Issue{
			Category:    CategoryMissingProviderBackend,
			Title:       "Providers Without Backend",
			Description: "Providers whose type has no installed plugin or built-in backend: requests fail and credentials never refresh. Install the missing plugin or clean to disable; re-enable after installing.",
			Count:       len(missingBackendKeys),
			Keys:        missingBackendKeys,
		})
	}

	// 2. Orphan Model Overrides & Model Infos. Override keys are
	// providerID/modelName (owned by modelinfo), so the check validates the
	// stored ProviderID value instead of splitting the key: key parsing
	// once flagged every live override as orphaned. Unparseable rows are
	// skipped, matching prior behavior.
	var orphanOverrideKeys []string
	var orphanInfoKeys []string
	if err := s.database.View(func(tx *bolt.Tx) error {
		overridesBucket := tx.Bucket(db.BucketModelOverrides)
		if overridesBucket != nil {
			_ = overridesBucket.ForEach(func(k, v []byte) error {
				var override models.ModelOverride
				if err := json.Unmarshal(v, &override); err != nil {
					return nil
				}
				if override.ProviderID == "" || !validProviders[override.ProviderID] {
					orphanOverrideKeys = append(orphanOverrideKeys, string(k))
				}
				return nil
			})
		}
		infosBucket := tx.Bucket(db.BucketModelInfos)
		if infosBucket != nil {
			_ = infosBucket.ForEach(func(k, v []byte) error {
				keyString := string(k)
				if !validProviders[keyString] {
					orphanInfoKeys = append(orphanInfoKeys, keyString)
				}
				return nil
			})
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("scan model overrides and infos: %w", err)
	}

	if len(orphanOverrideKeys) > 0 {
		issues = append(issues, Issue{
			Category:    CategoryOrphanModelOverrides,
			Title:       "Orphan Model Overrides",
			Description: "Model override settings for deleted providers",
			Count:       len(orphanOverrideKeys),
			Keys:        orphanOverrideKeys,
		})
	}

	if len(orphanInfoKeys) > 0 {
		issues = append(issues, Issue{
			Category:    CategoryOrphanModelInfos,
			Title:       "Orphan Model Caches",
			Description: "Cached model metadata for deleted providers",
			Count:       len(orphanInfoKeys),
			Keys:        orphanInfoKeys,
		})
	}

	// 3. Virtual Models issues (orphan managedBy or duplicate names)
	if virtualModels, err := s.virtualService.List(); err == nil {
		var orphanVirtualModelKeys []string
		var duplicateVirtualModelKeys []string
		nameCounts := map[string]int{}
		nameToID := map[string][]string{}

		for _, virtualModel := range virtualModels {
			lowerName := strings.ToLower(virtualModel.Name)
			nameCounts[lowerName]++
			nameToID[lowerName] = append(nameToID[lowerName], virtualModel.ID)

			if virtualModel.ManagedBy != "" {
				markerProvider, _, err := virtual.ParseMarker(virtualModel.ManagedBy)
				if err == nil && markerProvider != "" && !validProviders[markerProvider] {
					orphanVirtualModelKeys = append(orphanVirtualModelKeys, virtualModel.ID)
				}
			}
		}

		for name, count := range nameCounts {
			if count > 1 {
				duplicateVirtualModelKeys = append(duplicateVirtualModelKeys, nameToID[name]...)
			}
		}

		if len(orphanVirtualModelKeys) > 0 {
			issues = append(issues, Issue{
				Category:    CategoryOrphanVirtualModels,
				Title:       "Orphan Virtual Models",
				Description: "Managed virtual models associated with deleted providers",
				Count:       len(orphanVirtualModelKeys),
				Keys:        orphanVirtualModelKeys,
			})
		}

		if len(duplicateVirtualModelKeys) > 0 {
			issues = append(issues, Issue{
				Category:    CategoryDuplicateVirtualModels,
				Title:       "Duplicate Virtual Models",
				Description: "Multiple virtual models sharing the same name",
				Count:       len(duplicateVirtualModelKeys),
				Keys:        duplicateVirtualModelKeys,
			})
		}
	}

	// 5. Orphan Plugin Storage. Storage keys are pluginID/scope/key
	// joined by NUL bytes (luaplugin.ParseStorageKey). Rows for removed
	// plugins flag orphan and auto-fix deletes them. Unparseable rows form
	// their own inspect-only category: no writer builds them, so no one
	// can prove them orphaned, and Fix has no case for the category.
	var orphanStorageKeys []string
	var corruptStorageKeys []string
	if err := s.database.View(func(tx *bolt.Tx) error {
		storageBucket := tx.Bucket(db.BucketPluginStorage)
		if storageBucket != nil {
			_ = storageBucket.ForEach(func(k, v []byte) error {
				pluginID, _, _, ok := luaplugin.ParseStorageKey(string(k))
				if !ok {
					corruptStorageKeys = append(corruptStorageKeys, string(k))
					return nil
				}
				if !validPlugins[pluginID] {
					orphanStorageKeys = append(orphanStorageKeys, string(k))
				}
				return nil
			})
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("scan plugin storage: %w", err)
	}

	if len(orphanStorageKeys) > 0 {
		issues = append(issues, Issue{
			Category:    CategoryOrphanPluginStorage,
			Title:       "Orphan Plugin Storage",
			Description: "Stored key-value data for uninstalled plugins",
			Count:       len(orphanStorageKeys),
			Keys:        orphanStorageKeys,
		})
	}

	if len(corruptStorageKeys) > 0 {
		issues = append(issues, Issue{
			Category:    CategoryCorruptPluginStorage,
			Title:       "Corrupt Plugin Storage",
			Description: "Stored rows no writer builds: manual database review required, never auto-cleaned",
			Count:       len(corruptStorageKeys),
			Keys:        corruptStorageKeys,
		})
	}

	// 6. Broken Token Indexes
	var brokenIndexKeys []string
	if tokens, err := s.tokenService.List(); err == nil {
		validTokenIDs := map[string]bool{}
		for _, tokenRecord := range tokens {
			validTokenIDs[tokenRecord.ID] = true
		}
		if err := s.database.View(func(tx *bolt.Tx) error {
			indexBucket := tx.Bucket(db.BucketTokenIndex)
			if indexBucket != nil {
				_ = indexBucket.ForEach(func(k, v []byte) error {
					tokenID := string(v)
					if !validTokenIDs[tokenID] {
						brokenIndexKeys = append(brokenIndexKeys, string(k))
					}
					return nil
				})
			}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("scan token indexes: %w", err)
		}
	}

	if len(brokenIndexKeys) > 0 {
		issues = append(issues, Issue{
			Category:    CategoryBrokenTokenIndexes,
			Title:       "Broken Token Index Entries",
			Description: "Token lookup index entries referencing non-existent tokens",
			Count:       len(brokenIndexKeys),
			Keys:        brokenIndexKeys,
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

// Fix resolves issues for specified categories or all categories if categories is empty.
func (s *Service) Fix(categories []IssueCategory) (int, error) {
	report, err := s.Inspect()
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
				instance, err := s.providerService.Get(key)
				if err != nil || instance == nil {
					continue
				}
				if err := s.providerService.SystemDisable(key, "doctor: no installed plugin or built-in backend for type "+instance.TypeKey); err == nil {
					totalFixed++
				}
			}

		case CategoryOrphanCredentials:
			for _, key := range issue.Keys {
				if err := s.credentialService.Delete(key); err == nil {
					totalFixed++
				}
			}

		case CategoryOrphanModelOverrides:
			if err := s.database.Update(func(tx *bolt.Tx) error {
				bucket := tx.Bucket(db.BucketModelOverrides)
				if bucket != nil {
					for _, key := range issue.Keys {
						if err := bucket.Delete([]byte(key)); err == nil {
							totalFixed++
						}
					}
				}
				return nil
			}); err != nil {
				return totalFixed, fmt.Errorf("fix orphan model overrides: %w", err)
			}

		case CategoryOrphanModelInfos:
			if err := s.database.Update(func(tx *bolt.Tx) error {
				bucket := tx.Bucket(db.BucketModelInfos)
				if bucket != nil {
					for _, key := range issue.Keys {
						if err := bucket.Delete([]byte(key)); err == nil {
							totalFixed++
						}
					}
				}
				return nil
			}); err != nil {
				return totalFixed, fmt.Errorf("fix orphan model infos: %w", err)
			}

		case CategoryOrphanVirtualModels:
			for _, key := range issue.Keys {
				if err := s.virtualService.Delete(key); err == nil {
					totalFixed++
				}
			}

		case CategoryDuplicateVirtualModels:
			// Delete duplicates, keeping the first created or lowest ID
			for _, key := range issue.Keys {
				if err := s.virtualService.Delete(key); err == nil {
					totalFixed++
				}
			}

		case CategoryOrphanPluginStorage:
			if err := s.database.Update(func(tx *bolt.Tx) error {
				bucket := tx.Bucket(db.BucketPluginStorage)
				if bucket != nil {
					for _, key := range issue.Keys {
						if err := bucket.Delete([]byte(key)); err == nil {
							totalFixed++
						}
					}
				}
				return nil
			}); err != nil {
				return totalFixed, fmt.Errorf("fix orphan plugin storage: %w", err)
			}

		case CategoryBrokenTokenIndexes:
			if err := s.database.Update(func(tx *bolt.Tx) error {
				bucket := tx.Bucket(db.BucketTokenIndex)
				if bucket != nil {
					for _, key := range issue.Keys {
						if err := bucket.Delete([]byte(key)); err == nil {
							totalFixed++
						}
					}
				}
				return nil
			}); err != nil {
				return totalFixed, fmt.Errorf("fix broken token indexes: %w", err)
			}
		}
	}

	return totalFixed, nil
}
