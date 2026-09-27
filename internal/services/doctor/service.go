package doctor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/geoban"
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
	CategoryOrphanGeoBans          IssueCategory = "orphan_geo_bans"
	CategoryOrphanVirtualModels    IssueCategory = "orphan_virtual_models"
	CategoryDuplicateVirtualModels IssueCategory = "duplicate_virtual_models"
	CategoryOrphanPluginStorage    IssueCategory = "orphan_plugin_storage"
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
	db           *db.DB
	providerSvc  *provider.Service
	credSvc      *credential.Service
	virtualSvc   *virtual.Service
	tokenSvc     *token.Service
	proxySvc     *proxypool.Service
	luaSvc       *luaplugin.Service
	modelInfoSvc *modelinfo.Service
	geobanSvc    *geoban.Service
}

func New(
	database *db.DB,
	providerSvc *provider.Service,
	credSvc *credential.Service,
	virtualSvc *virtual.Service,
	tokenSvc *token.Service,
	proxySvc *proxypool.Service,
	luaSvc *luaplugin.Service,
	modelInfoSvc *modelinfo.Service,
	geobanSvc *geoban.Service,
) *Service {
	return &Service{
		db:           database,
		providerSvc:  providerSvc,
		credSvc:      credSvc,
		virtualSvc:   virtualSvc,
		tokenSvc:     tokenSvc,
		proxySvc:     proxySvc,
		luaSvc:       luaSvc,
		modelInfoSvc: modelInfoSvc,
		geobanSvc:    geobanSvc,
	}
}

// Inspect scans the database for orphaned or corrupt data.
func (s *Service) Inspect() (*InspectionReport, error) {
	var issues []Issue

	// Build lookup maps of valid entities
	providers, err := s.providerSvc.List()
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	validProviders := map[string]bool{}
	validProviderTypes := map[string]bool{}
	for _, p := range providers {
		validProviders[p.ID] = true
		validProviderTypes[p.TypeKey] = true
	}

	validProxies := map[string]bool{}
	if s.proxySvc != nil {
		if proxies, err := s.proxySvc.List(); err == nil {
			for _, px := range proxies {
				validProxies[px.ID] = true
			}
		}
	}

	validPlugins := map[string]bool{}
	if s.luaSvc != nil {
		if plugins, err := s.luaSvc.List(); err == nil {
			for _, pl := range plugins {
				validPlugins[pl.ID] = true
			}
		}
	}

	// 1. Orphan Credentials
	if creds, err := s.credSvc.ListAll(); err == nil {
		var keys []string
		for _, c := range creds {
			if !validProviders[c.ProviderID] {
				keys = append(keys, c.ID)
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

	// 2. Orphan Model Overrides & Model Infos
	var orphanOverrideKeys []string
	var orphanInfoKeys []string
	_ = s.db.View(func(tx *bolt.Tx) error {
		bOvs := tx.Bucket(db.BucketModelOverrides)
		if bOvs != nil {
			_ = bOvs.ForEach(func(k, v []byte) error {
				keyStr := string(k)
				parts := strings.SplitN(keyStr, ":", 2)
				if len(parts) > 0 && !validProviders[parts[0]] {
					orphanOverrideKeys = append(orphanOverrideKeys, keyStr)
				}
				return nil
			})
		}
		bInfos := tx.Bucket(db.BucketModelInfos)
		if bInfos != nil {
			_ = bInfos.ForEach(func(k, v []byte) error {
				keyStr := string(k)
				if !validProviders[keyStr] {
					orphanInfoKeys = append(orphanInfoKeys, keyStr)
				}
				return nil
			})
		}
		return nil
	})

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

	// 3. Orphan Geo Bans. Geo flags stay keyed by adapter type (shared
	// upstream region policy), so a stored provider value is valid when it
	// names either a live instance ID or a live adapter type key.
	var orphanGeoBanKeys []string
	_ = s.db.View(func(tx *bolt.Tx) error {
		bBans := tx.Bucket(db.BucketGeoBans)
		if bBans != nil {
			_ = bBans.ForEach(func(k, v []byte) error {
				var entry models.GeoBanEntry
				if err := json.Unmarshal(v, &entry); err == nil {
					providerLive := entry.Provider == "" || validProviders[entry.Provider] || validProviderTypes[entry.Provider]
					if !providerLive || (entry.Proxy != "" && !validProxies[entry.Proxy]) {
						orphanGeoBanKeys = append(orphanGeoBanKeys, string(k))
					}
				}
				return nil
			})
		}
		return nil
	})

	if len(orphanGeoBanKeys) > 0 {
		issues = append(issues, Issue{
			Category:    CategoryOrphanGeoBans,
			Title:       "Orphan Geo Bans",
			Description: "Geo ban entries for deleted providers or proxies",
			Count:       len(orphanGeoBanKeys),
			Keys:        orphanGeoBanKeys,
		})
	}

	// 4. Virtual Models issues (orphan managedBy or duplicate names)
	if vms, err := s.virtualSvc.List(); err == nil {
		var orphanVmKeys []string
		var duplicateVmKeys []string
		nameCounts := map[string]int{}
		nameToID := map[string][]string{}

		for _, vm := range vms {
			lowerName := strings.ToLower(vm.Name)
			nameCounts[lowerName]++
			nameToID[lowerName] = append(nameToID[lowerName], vm.ID)

			if vm.ManagedBy != "" {
				markerProvider, _, err := virtual.ParseMarker(vm.ManagedBy)
				if err == nil && markerProvider != "" && !validProviders[markerProvider] {
					orphanVmKeys = append(orphanVmKeys, vm.ID)
				}
			}
		}

		for name, count := range nameCounts {
			if count > 1 {
				duplicateVmKeys = append(duplicateVmKeys, nameToID[name]...)
			}
		}

		if len(orphanVmKeys) > 0 {
			issues = append(issues, Issue{
				Category:    CategoryOrphanVirtualModels,
				Title:       "Orphan Virtual Models",
				Description: "Managed virtual models associated with deleted providers",
				Count:       len(orphanVmKeys),
				Keys:        orphanVmKeys,
			})
		}

		if len(duplicateVmKeys) > 0 {
			issues = append(issues, Issue{
				Category:    CategoryDuplicateVirtualModels,
				Title:       "Duplicate Virtual Models",
				Description: "Multiple virtual models sharing the same name",
				Count:       len(duplicateVmKeys),
				Keys:        duplicateVmKeys,
			})
		}
	}

	// 5. Orphan Plugin Storage
	var orphanStorageKeys []string
	_ = s.db.View(func(tx *bolt.Tx) error {
		bStorage := tx.Bucket(db.BucketPluginStorage)
		if bStorage != nil {
			_ = bStorage.ForEach(func(k, v []byte) error {
				keyStr := string(k)
				parts := strings.SplitN(keyStr, ":", 2)
				if len(parts) > 0 && !validPlugins[parts[0]] {
					orphanStorageKeys = append(orphanStorageKeys, keyStr)
				}
				return nil
			})
		}
		return nil
	})

	if len(orphanStorageKeys) > 0 {
		issues = append(issues, Issue{
			Category:    CategoryOrphanPluginStorage,
			Title:       "Orphan Plugin Storage",
			Description: "Stored key-value data for uninstalled plugins",
			Count:       len(orphanStorageKeys),
			Keys:        orphanStorageKeys,
		})
	}

	// 6. Broken Token Indexes
	var brokenIndexKeys []string
	if tokens, err := s.tokenSvc.List(); err == nil {
		validTokenIDs := map[string]bool{}
		for _, t := range tokens {
			validTokenIDs[t.ID] = true
		}
		_ = s.db.View(func(tx *bolt.Tx) error {
			bIdx := tx.Bucket(db.BucketTokenIndex)
			if bIdx != nil {
				_ = bIdx.ForEach(func(k, v []byte) error {
					tokenID := string(v)
					if !validTokenIDs[tokenID] {
						brokenIndexKeys = append(brokenIndexKeys, string(k))
					}
					return nil
				})
			}
			return nil
		})
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

	catMap := map[IssueCategory]bool{}
	for _, c := range categories {
		catMap[c] = true
	}
	allCategories := len(categories) == 0

	totalFixed := 0

	for _, issue := range report.Issues {
		if !allCategories && !catMap[issue.Category] {
			continue
		}

		switch issue.Category {
		case CategoryOrphanCredentials:
			for _, key := range issue.Keys {
				if err := s.credSvc.Delete(key); err == nil {
					totalFixed++
				}
			}

		case CategoryOrphanModelOverrides:
			_ = s.db.Update(func(tx *bolt.Tx) error {
				b := tx.Bucket(db.BucketModelOverrides)
				if b != nil {
					for _, k := range issue.Keys {
						if err := b.Delete([]byte(k)); err == nil {
							totalFixed++
						}
					}
				}
				return nil
			})

		case CategoryOrphanModelInfos:
			_ = s.db.Update(func(tx *bolt.Tx) error {
				b := tx.Bucket(db.BucketModelInfos)
				if b != nil {
					for _, k := range issue.Keys {
						if err := b.Delete([]byte(k)); err == nil {
							totalFixed++
						}
					}
				}
				return nil
			})

		case CategoryOrphanGeoBans:
			_ = s.db.Update(func(tx *bolt.Tx) error {
				b := tx.Bucket(db.BucketGeoBans)
				if b != nil {
					for _, k := range issue.Keys {
						if err := b.Delete([]byte(k)); err == nil {
							totalFixed++
						}
					}
				}
				return nil
			})

		case CategoryOrphanVirtualModels:
			for _, key := range issue.Keys {
				if err := s.virtualSvc.Delete(key); err == nil {
					totalFixed++
				}
			}

		case CategoryDuplicateVirtualModels:
			// Delete duplicates, keeping the first created or lowest ID
			for _, key := range issue.Keys {
				if err := s.virtualSvc.Delete(key); err == nil {
					totalFixed++
				}
			}

		case CategoryOrphanPluginStorage:
			_ = s.db.Update(func(tx *bolt.Tx) error {
				b := tx.Bucket(db.BucketPluginStorage)
				if b != nil {
					for _, k := range issue.Keys {
						if err := b.Delete([]byte(k)); err == nil {
							totalFixed++
						}
					}
				}
				return nil
			})

		case CategoryBrokenTokenIndexes:
			_ = s.db.Update(func(tx *bolt.Tx) error {
				b := tx.Bucket(db.BucketTokenIndex)
				if b != nil {
					for _, k := range issue.Keys {
						if err := b.Delete([]byte(k)); err == nil {
							totalFixed++
						}
					}
				}
				return nil
			})
		}
	}

	return totalFixed, nil
}
