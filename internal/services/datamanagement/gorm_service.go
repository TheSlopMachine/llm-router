// GORM-backed data management.
//
// GormService orchestrates export, import, and truncation against
// context-taking dependencies: GORM-backed providers, credentials, virtual
// models, tokens, plugins, repositories, and model overrides. It owns no
// tables and issues no storage statements itself: every read and write
// delegates to the owning service through narrow interfaces, so bundle
// shapes stay stable.
package datamanagement

import (
	"context"
	"fmt"
	"log/slog"

	stdErrors "errors"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/pluginrepo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
)

// Aliases for clean API import payloads
type VirtualModelImport = models.VirtualModel
type TokenImport = models.RouterToken

// ProviderBundle packages a provider instance with its credentials and model overrides.
type ProviderBundle struct {
	Instance    *models.ProviderInstance `json:"instance"`
	Credentials []*models.Credential     `json:"credentials"`
	Overrides   []*models.ModelOverride  `json:"overrides,omitempty"`
}

// SubsystemStats reports record counts for data management UI.
type SubsystemStats struct {
	Providers     int `json:"providers"`
	Credentials   int `json:"credentials"`
	VirtualModels int `json:"virtual_models"`
	Plugins       int `json:"plugins"`
	PluginRepos   int `json:"plugin_repos"`
	Tokens        int `json:"tokens"`
	ProxyPools    int `json:"proxy_pools"`
	Proxies       int `json:"proxies"`
}

// PluginsExportBundle packages plugin store repositories with installed plugins.
type PluginsExportBundle struct {
	Repos   []*pluginrepo.RepoRecord  `json:"repos"`
	Plugins []*luaplugin.PluginRecord `json:"plugins"`
}

// GormProviderBackend is the provider surface the GORM data management path
// needs: inventory reads plus the create/update/delete writes imports and
// purges drive. Signatures match the provider GORM service.
type GormProviderBackend interface {
	Get(ctx context.Context, identifier string) (*models.ProviderInstance, error)
	List(ctx context.Context) ([]*models.ProviderInstance, error)
	Create(ctx context.Context, options provider.CreateOptions) (*models.ProviderInstance, error)
	Update(ctx context.Context, identifier string, options provider.UpdateOptions) (*models.ProviderInstance, error)
	Delete(ctx context.Context, identifier string) error
}

// GormCredentialBackend is the credential surface the GORM data management
// path needs: inventory reads plus the add/update writes provider imports
// drive. Signatures match the credential GORM service.
type GormCredentialBackend interface {
	ListAll(ctx context.Context) ([]*models.Credential, error)
	ListByProvider(ctx context.Context, providerID string) ([]*models.Credential, error)
	UpdateDetails(ctx context.Context, identifier string, label *string, disabled *bool, data map[string]any) error
	Add(ctx context.Context, options credential.AddOptions) (*models.Credential, error)
}

// GormVirtualBackend is the virtual model surface the GORM data management
// path needs. Signatures match the virtual GORM service.
type GormVirtualBackend interface {
	List(ctx context.Context) ([]*models.VirtualModel, error)
	Get(ctx context.Context, identifier string) (*models.VirtualModel, error)
	Create(ctx context.Context, virtualModel *models.VirtualModel) error
	Update(ctx context.Context, identifier string, virtualModel *models.VirtualModel) error
	Delete(ctx context.Context, identifier string) error
}

// GormTokenBackend is the token surface the GORM data management path
// needs. Signatures match the token GORM service.
type GormTokenBackend interface {
	List(ctx context.Context) ([]*models.RouterToken, error)
	UpdateRules(ctx context.Context, identifier string, rules models.TokenRules) error
	Create(ctx context.Context, options token.CreateOptions) (*models.RouterToken, error)
	Delete(ctx context.Context, identifier string) error
}

// GormProxyBackend is the proxy subsystem persistence the data service needs.
type GormProxyBackend interface {
	DataCounts(ctx context.Context) (pools, proxies int, err error)
	ExportData(ctx context.Context) (*models.ProxyDataBundle, error)
	ImportData(ctx context.Context, bundle *models.ProxyDataBundle) error
	ClearData(ctx context.Context) error
}

// GormModelInfoBackend is the model metadata surface the GORM data
// management path needs: override reads/writes plus snapshot invalidation
// on provider purge. Signatures match the modelinfo GORM service.
type GormModelInfoBackend interface {
	ListOverrides(ctx context.Context, providerID string) ([]*models.ModelOverride, error)
	SetOverride(ctx context.Context, override models.ModelOverride) error
	DeleteOverride(ctx context.Context, providerID, modelName string) error
	DeleteSnapshot(ctx context.Context, providerID string) error
}

// GormPluginBackend is the plugin surface the GORM data management path
// needs: SQLite list/install/delete plus the disabled toggle. Signatures
// match the luaplugin SQLite methods; storage rows cascade from the plugin
// delete, so no storage statement runs here.
type GormPluginBackend interface {
	ListSQLite(ctx context.Context) ([]*luaplugin.PluginRecord, error)
	InstallSQLite(ctx context.Context, source []byte, origin luaplugin.PluginOrigin) (*luaplugin.PluginRecord, error)
	SetDisabled(ctx context.Context, pluginID string, disabled bool) (*luaplugin.PluginRecord, error)
	DeleteSQLite(ctx context.Context, pluginID string) error
}

// GormRepositoryBackend is the repository surface the GORM data management
// path needs. The pluginrepo Service already mirrors writes to SQLite, so
// the same context-free List/Remove plus context-taking AddRepo cover both
// paths.
type GormRepositoryBackend interface {
	List() ([]*pluginrepo.RepoRecord, error)
	AddRepo(ctx context.Context, rawURL string) (*pluginrepo.RepoRecord, error)
	Remove(identifier string) error
}

// Compile-time proof that the GORM-backed services plug into the narrow
// data management interfaces without involving their package internals.
var (
	_ GormProviderBackend   = (*provider.GormService)(nil)
	_ GormCredentialBackend = (*credential.GormService)(nil)
	_ GormVirtualBackend    = (*virtual.GormService)(nil)
	_ GormTokenBackend      = (*token.GormService)(nil)
	_ GormModelInfoBackend  = (*modelinfo.GormService)(nil)
	_ GormPluginBackend     = (*luaplugin.Service)(nil)
	_ GormRepositoryBackend = (*pluginrepo.Service)(nil)
)

// GormService orchestrates subsystem export, import, and truncation against
// GORM-backed dependencies. Construction performs no I/O. Nil plugin,
// repository, model info, virtual, and token backends disable their
// subsystems exactly as before.
type GormService struct {
	providerSource   GormProviderBackend
	credentialSource GormCredentialBackend
	virtualSource    GormVirtualBackend
	tokenSource      GormTokenBackend
	pluginSource     GormPluginBackend
	repositorySource GormRepositoryBackend
	modelInfoSource  GormModelInfoBackend
	proxySource      GormProxyBackend
	logger           *slog.Logger
}

// SetProxyBackend wires the proxy subsystem. Without it the proxy data
// operations report an error.
func (s *GormService) SetProxyBackend(backend GormProxyBackend) {
	s.proxySource = backend
}

// NewGorm constructs a GORM-backed data management service. Construction
// performs no I/O.
func NewGorm(
	providerSource GormProviderBackend,
	credentialSource GormCredentialBackend,
	virtualSource GormVirtualBackend,
	tokenSource GormTokenBackend,
	pluginSource GormPluginBackend,
	repositorySource GormRepositoryBackend,
	modelInfoSource GormModelInfoBackend,
) *GormService {
	return &GormService{
		providerSource:   providerSource,
		credentialSource: credentialSource,
		virtualSource:    virtualSource,
		tokenSource:      tokenSource,
		pluginSource:     pluginSource,
		repositorySource: repositorySource,
		modelInfoSource:  modelInfoSource,
	}
}

// SetLogger wires structured logging.
func (s *GormService) SetLogger(logger *slog.Logger) {
	if s == nil {
		return
	}
	s.logger = logger
}

// Stats returns the record counts for each managed subsystem. A failing
// subsystem contributes no count, matching the previous behavior: stats stay
// best-effort so one broken subsystem never hides the rest.
func (s *GormService) Stats(ctx context.Context) (SubsystemStats, error) {
	var statistics SubsystemStats
	if providers, err := s.providerSource.List(ctx); err == nil {
		statistics.Providers = len(providers)
	} else if s != nil && s.logger != nil {
		s.logger.Warn("Stats providers leg failed", "subsystem", "providers", "error", err)
	}
	if credentials, err := s.credentialSource.ListAll(ctx); err == nil {
		statistics.Credentials = len(credentials)
	} else if s != nil && s.logger != nil {
		s.logger.Warn("Stats credentials leg failed", "subsystem", "credentials", "error", err)
	}
	if s.virtualSource != nil {
		if virtualModels, err := s.virtualSource.List(ctx); err == nil {
			statistics.VirtualModels = len(virtualModels)
		} else if s != nil && s.logger != nil {
			s.logger.Warn("Stats virtual models leg failed", "subsystem", "virtual_models", "error", err)
		}
	}
	if s.tokenSource != nil {
		if tokens, err := s.tokenSource.List(ctx); err == nil {
			statistics.Tokens = len(tokens)
		} else if s != nil && s.logger != nil {
			s.logger.Warn("Stats tokens leg failed", "subsystem", "tokens", "error", err)
		}
	}
	if s.pluginSource != nil {
		if plugins, err := s.pluginSource.ListSQLite(ctx); err == nil {
			statistics.Plugins = len(plugins)
		} else if s != nil && s.logger != nil {
			s.logger.Warn("Stats plugins leg failed", "subsystem", "plugins", "error", err)
		}
	}
	if s.proxySource != nil {
		if pools, proxies, err := s.proxySource.DataCounts(ctx); err == nil {
			statistics.ProxyPools = pools
			statistics.Proxies = proxies
		} else if s != nil && s.logger != nil {
			s.logger.Warn("Stats proxy leg failed", "subsystem", "proxy", "error", err)
		}
	}
	if s.repositorySource != nil {
		if repositories, err := s.repositorySource.List(); err == nil {
			statistics.PluginRepos = len(repositories)
		} else if s != nil && s.logger != nil {
			s.logger.Warn("Stats plugin repositories leg failed", "subsystem", "plugin_repos", "error", err)
		}
	}
	return statistics, nil
}

// ─────────────────────────────────────────────
// Providers
// ─────────────────────────────────────────────

// ExportProviders packages every provider instance with its credentials and
// model overrides. Override list failures yield no overrides for that
// provider, matching the previous behavior: overrides stay optional metadata.
func (s *GormService) ExportProviders(ctx context.Context) ([]*ProviderBundle, error) {
	providers, err := s.providerSource.List(ctx)
	if err != nil {
		return nil, err
	}
	credentials, err := s.credentialSource.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	credentialsByProvider := map[string][]*models.Credential{}
	for _, credentialItem := range credentials {
		credentialsByProvider[credentialItem.ProviderID] = append(credentialsByProvider[credentialItem.ProviderID], credentialItem)
	}
	bundles := make([]*ProviderBundle, 0, len(providers))
	for _, providerInstance := range providers {
		var overrides []*models.ModelOverride
		if s.modelInfoSource != nil {
			if fetchedOverrides, err := s.modelInfoSource.ListOverrides(ctx, providerInstance.ID); err == nil {
				overrides = fetchedOverrides
			} else if s != nil && s.logger != nil {
				s.logger.Warn("Export providers override fallback", "subsystem", "providers", "provider_id", providerInstance.ID, "error", err)
			}
		}
		bundles = append(bundles, &ProviderBundle{
			Instance:    providerInstance,
			Credentials: credentialsByProvider[providerInstance.ID],
			Overrides:   overrides,
		})
	}
	if s != nil && s.logger != nil {
		s.logger.Info("Export providers completed", "subsystem", "providers", "count", len(bundles))
	}
	return bundles, nil
}

// ImportProviders creates or updates every bundled provider, then its
// credentials and overrides. Update failures return immediately; create
// failures return immediately instead of surfacing later at the credential
// write.
func (s *GormService) ImportProviders(ctx context.Context, bundles []*ProviderBundle) error {
	createdCount := 0
	updatedCount := 0
	skippedCount := 0
	for _, bundle := range bundles {
		if bundle == nil {
			skippedCount++
			if s != nil && s.logger != nil {
				s.logger.Warn("Import providers skipped bundle", "subsystem", "providers", "skipped_count", skippedCount, "reason", "nil_bundle")
			}
			continue
		}
		if bundle.Instance == nil {
			skippedCount++
			if s != nil && s.logger != nil {
				s.logger.Warn("Import providers skipped bundle", "subsystem", "providers", "skipped_count", skippedCount, "reason", "nil_instance")
			}
			continue
		}
		instance := bundle.Instance
		if instance.ID == "" {
			skippedCount++
			if s != nil && s.logger != nil {
				s.logger.Warn("Import providers skipped bundle", "subsystem", "providers", "skipped_count", skippedCount, "reason", "empty_id")
			}
			continue
		}
		completedSoFar := createdCount + updatedCount
		existing, err := s.providerSource.Get(ctx, instance.ID)
		if err == nil && existing != nil {
			if _, err := s.providerSource.Update(ctx, instance.ID, provider.UpdateOptions{
				Name:     instance.Name,
				Config:   instance.Config,
				IconURL:  instance.IconURL,
				Disabled: &instance.Disabled,
			}); err != nil {
				if s != nil && s.logger != nil {
					s.logger.Error("Import providers aborted", "subsystem", "providers", "provider_id", instance.ID, "completed_so_far", completedSoFar, "error", err)
				}
				return err
			}
			updatedCount++
		} else {
			// Create regenerates the ID (dedup suffixes on collision), so
			// credentials and overrides below must follow the created
			// instance, not the bundle's: otherwise they point at an ID
			// that was never stored.
			created, createError := s.providerSource.Create(ctx, provider.CreateOptions{
				Name:      instance.Name,
				TypeKey:   instance.TypeKey,
				Qualifier: instance.Qualifier,
				Config:    instance.Config,
				IconURL:   instance.IconURL,
			})
			if createError != nil {
				if s != nil && s.logger != nil {
					s.logger.Error("Import providers aborted", "subsystem", "providers", "provider_id", instance.ID, "completed_so_far", completedSoFar, "error", createError)
				}
				return createError
			}
			if created != nil {
				instance = created
			}
			createdCount++
		}
		for _, credentialItem := range bundle.Credentials {
			if credentialItem == nil {
				skippedCount++
				if s != nil && s.logger != nil {
					s.logger.Warn("Import providers skipped credential", "subsystem", "providers", "provider_id", instance.ID, "skipped_count", skippedCount, "reason", "nil_credential")
				}
				continue
			}
			credentialItem.ProviderID = instance.ID
			if credentialItem.ID == "" || s.credentialSource.UpdateDetails(ctx, credentialItem.ID, &credentialItem.Label, &credentialItem.Disabled, credentialItem.Data) != nil {
				if _, err := s.credentialSource.Add(ctx, credential.AddOptions{
					ProviderID: instance.ID,
					Label:      credentialItem.Label,
					Data:       credentialItem.Data,
				}); err != nil {
					if s != nil && s.logger != nil {
						s.logger.Error("Import providers aborted", "subsystem", "providers", "provider_id", instance.ID, "completed_so_far", createdCount+updatedCount, "error", err)
					}
					return err
				}
			}
		}
		if s.modelInfoSource != nil {
			for _, override := range bundle.Overrides {
				if override == nil || override.Name == "" {
					skippedCount++
					if s != nil && s.logger != nil {
						s.logger.Warn("Import providers skipped override", "subsystem", "providers", "provider_id", instance.ID, "skipped_count", skippedCount, "reason", "nil_override")
					}
					continue
				}
				override.ProviderID = instance.ID
				if err := s.modelInfoSource.SetOverride(ctx, *override); err != nil {
					if s != nil && s.logger != nil {
						s.logger.Error("Import providers aborted", "subsystem", "providers", "provider_id", instance.ID, "completed_so_far", createdCount+updatedCount, "error", err)
					}
					return err
				}
			}
		}
	}
	if s != nil && s.logger != nil {
		s.logger.Info("Import providers completed", "subsystem", "providers", "created", createdCount, "updated", updatedCount, "skipped", skippedCount)
	}
	return nil
}

// ExportProvider packages one provider instance with its credentials and
// overrides. Credential and override list failures yield empty lists,
// matching the previous behavior.
func (s *GormService) ExportProvider(ctx context.Context, identifier string) (*ProviderBundle, error) {
	providerInstance, err := s.providerSource.Get(ctx, identifier)
	if err != nil {
		return nil, err
	}
	var credentials []*models.Credential
	if fetchedCredentials, err := s.credentialSource.ListByProvider(ctx, identifier); err == nil {
		credentials = fetchedCredentials
	} else if s != nil && s.logger != nil {
		s.logger.Warn("Export provider credential fallback", "subsystem", "providers", "provider_id", identifier, "error", err)
	}
	var overrides []*models.ModelOverride
	if s.modelInfoSource != nil {
		if fetchedOverrides, err := s.modelInfoSource.ListOverrides(ctx, identifier); err == nil {
			overrides = fetchedOverrides
		} else if s != nil && s.logger != nil {
			s.logger.Warn("Export provider override fallback", "subsystem", "providers", "provider_id", identifier, "error", err)
		}
	}
	return &ProviderBundle{
		Instance:    providerInstance,
		Credentials: credentials,
		Overrides:   overrides,
	}, nil
}

// ImportProvider imports a single provider bundle.
func (s *GormService) ImportProvider(ctx context.Context, bundle *ProviderBundle) error {
	if bundle == nil || bundle.Instance == nil {
		return fmt.Errorf("empty provider bundle")
	}
	return s.ImportProviders(ctx, []*ProviderBundle{bundle})
}

// PurgeProvider removes a provider instance and all associated data:
// credentials cascade from the provider delete, snapshots and overrides go
// through the owning model info service, and managed virtual models delete
// through the virtual service. A missing snapshot is not an error,
// matching the idempotent previous invalidation.
func (s *GormService) PurgeProvider(ctx context.Context, identifier string) error {
	if _, err := s.providerSource.Get(ctx, identifier); err != nil {
		return err
	}
	// 1. Delete provider instance and cascade credentials.
	if err := s.providerSource.Delete(ctx, identifier); err != nil {
		if s != nil && s.logger != nil {
			s.logger.Error("Purge provider aborted", "subsystem", "providers", "provider_id", identifier, "completed_so_far", 0, "error", err)
		}
		return err
	}
	removedCount := 0
	// 2. Delete model snapshots and overrides through the owning service.
	if s.modelInfoSource != nil {
		if err := s.modelInfoSource.DeleteSnapshot(ctx, identifier); err != nil {
			if !stdErrors.Is(err, apierrors.ErrNotFound) {
				if s != nil && s.logger != nil {
					s.logger.Error("Purge provider aborted", "subsystem", "providers", "provider_id", identifier, "completed_so_far", 1, "error", err)
				}
				return err
			}
		}
		fetchedOverrides, err := s.modelInfoSource.ListOverrides(ctx, identifier)
		if err != nil {
			if s != nil && s.logger != nil {
				s.logger.Error("Purge provider aborted", "subsystem", "providers", "provider_id", identifier, "completed_so_far", 1, "error", err)
			}
			return err
		}
		for _, override := range fetchedOverrides {
			if override == nil {
				continue
			}
			if err := s.modelInfoSource.DeleteOverride(ctx, identifier, override.Name); err != nil {
				if s != nil && s.logger != nil {
					s.logger.Error("Purge provider aborted", "subsystem", "providers", "provider_id", identifier, "completed_so_far", 1+removedCount, "error", err)
				}
				return err
			}
			removedCount++
		}
	}
	// 3. Delete managed virtual models associated with this provider.
	if s.virtualSource != nil {
		virtualModels, err := s.virtualSource.List(ctx)
		if err != nil {
			if s != nil && s.logger != nil {
				s.logger.Error("Purge provider aborted", "subsystem", "providers", "provider_id", identifier, "completed_so_far", 1+removedCount, "error", err)
			}
			return err
		}
		for _, virtualModel := range virtualModels {
			if virtualModel == nil {
				continue
			}
			if virtualModel.ManagedBy == "" {
				continue
			}
			markerProvider, _, err := virtual.ParseMarker(virtualModel.ManagedBy)
			if err != nil {
				if s != nil && s.logger != nil {
					s.logger.Error("Purge provider aborted", "subsystem", "providers", "provider_id", identifier, "completed_so_far", 1+removedCount, "error", err)
				}
				return err
			}
			if markerProvider == identifier {
				if err := s.virtualSource.Delete(ctx, virtualModel.ID); err != nil {
					if s != nil && s.logger != nil {
						s.logger.Error("Purge provider aborted", "subsystem", "providers", "provider_id", identifier, "completed_so_far", 1+removedCount, "error", err)
					}
					return err
				}
				removedCount++
			}
		}
	}
	if s != nil && s.logger != nil {
		s.logger.Info("Purge provider completed", "subsystem", "providers", "provider_id", identifier, "deleted", 1+removedCount)
	}
	return nil
}

// ClearProviders deletes every provider instance. Credential, park, health,
// override, info, managed virtual, and member rows cascade from the provider
// foreign keys with no scans.
func (s *GormService) ClearProviders(ctx context.Context) error {
	providers, err := s.providerSource.List(ctx)
	if err != nil {
		return err
	}
	deletedCount := 0
	for _, providerInstance := range providers {
		if err := s.providerSource.Delete(ctx, providerInstance.ID); err != nil {
			if s != nil && s.logger != nil {
				s.logger.Error("Clear providers aborted", "subsystem", "providers", "provider_id", providerInstance.ID, "completed_so_far", deletedCount, "error", err)
			}
			return err
		}
		deletedCount++
	}
	if s != nil && s.logger != nil {
		s.logger.Info("Clear providers completed", "subsystem", "providers", "deleted", deletedCount)
	}
	return nil
}

// ─────────────────────────────────────────────
// Virtual Models
// ─────────────────────────────────────────────

// ExportVirtualModels returns every virtual model.
func (s *GormService) ExportVirtualModels(ctx context.Context) ([]*models.VirtualModel, error) {
	all, err := s.virtualSource.List(ctx)
	if err != nil {
		return nil, err
	}
	// Provider-managed models are computed from the provider list and come
	// back on their own; only manually maintained ones are data.
	virtualModels := make([]*models.VirtualModel, 0, len(all))
	for _, virtualModel := range all {
		if virtualModel != nil && virtualModel.ManagedBy == "" {
			virtualModels = append(virtualModels, virtualModel)
		}
	}
	if s != nil && s.logger != nil {
		s.logger.Info("Export virtual models completed", "subsystem", "virtual_models", "count", len(virtualModels), "managed_skipped", len(all)-len(virtualModels))
	}
	return virtualModels, nil
}

// ImportVirtualModels creates or updates every virtual model. Update
// failures return immediately instead of falling through to a duplicate
// create.
func (s *GormService) ImportVirtualModels(ctx context.Context, virtualModels []*models.VirtualModel) error {
	createdCount := 0
	updatedCount := 0
	skippedCount := 0
	for _, virtualModel := range virtualModels {
		if virtualModel != nil && virtualModel.ManagedBy != "" {
			// Older exports carry provider-managed models. They are rebuilt
			// from the provider list and may reference missing providers.
			skippedCount++
			continue
		}
		if virtualModel == nil || virtualModel.Name == "" {
			skippedCount++
			if s != nil && s.logger != nil {
				s.logger.Warn("Import virtual models skipped entry", "subsystem", "virtual_models", "skipped_count", skippedCount, "reason", "nil_entry")
			}
			continue
		}
		if virtualModel.ID != "" {
			existing, err := s.virtualSource.Get(ctx, virtualModel.ID)
			if err == nil && existing != nil {
				if err := s.virtualSource.Update(ctx, virtualModel.ID, virtualModel); err != nil {
					if s != nil && s.logger != nil {
						s.logger.Error("Import virtual models aborted", "subsystem", "virtual_models", "completed_so_far", createdCount+updatedCount, "error", err)
					}
					return err
				}
				updatedCount++
				continue
			}
		}
		if err := s.virtualSource.Create(ctx, virtualModel); err != nil {
			if s != nil && s.logger != nil {
				s.logger.Error("Import virtual models aborted", "subsystem", "virtual_models", "completed_so_far", createdCount+updatedCount, "error", err)
			}
			return err
		}
		createdCount++
	}
	if s != nil && s.logger != nil {
		s.logger.Info("Import virtual models completed", "subsystem", "virtual_models", "created", createdCount, "updated", updatedCount, "skipped", skippedCount)
	}
	return nil
}

// ClearVirtualModels deletes every virtual model. Member rows cascade from
// the model delete with no scans.
func (s *GormService) ClearVirtualModels(ctx context.Context) error {
	virtualModels, err := s.virtualSource.List(ctx)
	if err != nil {
		return err
	}
	deletedCount := 0
	for _, virtualModel := range virtualModels {
		if err := s.virtualSource.Delete(ctx, virtualModel.ID); err != nil {
			if s != nil && s.logger != nil {
				s.logger.Error("Clear virtual models aborted", "subsystem", "virtual_models", "completed_so_far", deletedCount, "error", err)
			}
			return err
		}
		deletedCount++
	}
	if s != nil && s.logger != nil {
		s.logger.Info("Clear virtual models completed", "subsystem", "virtual_models", "deleted", deletedCount)
	}
	return nil
}

// ─────────────────────────────────────────────
// Plugins & Repos
// ─────────────────────────────────────────────

// ExportPlugins packages repository records plus installed plugin records.
// List failures yield empty halves, matching the previous behavior.
func (s *GormService) ExportPlugins(ctx context.Context) (*PluginsExportBundle, error) {
	var bundle PluginsExportBundle
	if s.repositorySource != nil {
		if repositories, err := s.repositorySource.List(); err == nil {
			bundle.Repos = repositories
		} else if s != nil && s.logger != nil {
			s.logger.Warn("Export plugins repository fallback", "subsystem", "plugin_repos", "error", err)
		}
	}
	if s.pluginSource != nil {
		if plugins, err := s.pluginSource.ListSQLite(ctx); err == nil {
			bundle.Plugins = plugins
		} else if s != nil && s.logger != nil {
			s.logger.Warn("Export plugins fallback", "subsystem", "plugins", "error", err)
		}
	}
	if s != nil && s.logger != nil {
		s.logger.Info("Export plugins completed", "subsystem", "plugins", "count", len(bundle.Plugins)+len(bundle.Repos))
	}
	return &bundle, nil
}

// ImportPlugins adds every bundled repository, then installs every bundled
// plugin source. Failures return immediately.
func (s *GormService) ImportPlugins(ctx context.Context, bundle *PluginsExportBundle) error {
	if bundle == nil {
		return nil
	}
	createdCount := 0
	skippedCount := 0
	if s.repositorySource != nil {
		for _, repository := range bundle.Repos {
			if repository == nil {
				skippedCount++
				if s != nil && s.logger != nil {
					s.logger.Warn("Import plugins skipped repository", "subsystem", "plugin_repos", "skipped_count", skippedCount, "reason", "nil_repository")
				}
				continue
			}
			targetURL := repository.IndexURL
			if targetURL == "" {
				targetURL = repository.SourceURL
			}
			if targetURL == "" {
				skippedCount++
				if s != nil && s.logger != nil {
					s.logger.Warn("Import plugins skipped repository", "subsystem", "plugin_repos", "skipped_count", skippedCount, "reason", "empty_url")
				}
				continue
			}
			if _, err := s.repositorySource.AddRepo(ctx, targetURL); err != nil {
				if s != nil && s.logger != nil {
					s.logger.Error("Import plugins aborted", "subsystem", "plugin_repos", "completed_so_far", createdCount, "error", err)
				}
				return err
			}
			createdCount++
		}
	}
	if s.pluginSource != nil {
		for _, plugin := range bundle.Plugins {
			if plugin == nil || len(plugin.Source) == 0 {
				skippedCount++
				if s != nil && s.logger != nil {
					s.logger.Warn("Import plugins skipped plugin", "subsystem", "plugins", "skipped_count", skippedCount, "reason", "empty_source")
				}
				continue
			}
			installed, err := s.pluginSource.InstallSQLite(ctx, plugin.Source, plugin.Origin)
			if err != nil {
				if s != nil && s.logger != nil {
					s.logger.Error("Import plugins aborted", "subsystem", "plugins", "completed_so_far", createdCount, "error", err)
				}
				return err
			}
			if plugin.Disabled && !installed.Disabled {
				if _, err := s.pluginSource.SetDisabled(ctx, installed.ID, true); err != nil {
					if s != nil && s.logger != nil {
						s.logger.Error("Import plugins aborted", "subsystem", "plugins", "completed_so_far", createdCount, "error", err)
					}
					return err
				}
			}
			createdCount++
		}
	}
	if s != nil && s.logger != nil {
		s.logger.Info("Import plugins completed", "subsystem", "plugins", "created", createdCount, "skipped", skippedCount)
	}
	return nil
}

// ClearPlugins deletes every plugin and every non-builtin repository.
// Plugin storage, allow host, and version rows cascade from the plugin
// delete in the owning luaplugin service; this package never touches tables
// outside its own directory.
func (s *GormService) ClearPlugins(ctx context.Context) error {
	deletedCount := 0
	skippedCount := 0
	if s.pluginSource != nil {
		plugins, err := s.pluginSource.ListSQLite(ctx)
		if err != nil {
			return err
		}
		for _, plugin := range plugins {
			if err := s.pluginSource.DeleteSQLite(ctx, plugin.ID); err != nil {
				if s != nil && s.logger != nil {
					s.logger.Error("Clear plugins aborted", "subsystem", "plugins", "completed_so_far", deletedCount, "error", err)
				}
				return err
			}
			deletedCount++
		}
	}
	if s.repositorySource != nil {
		repositories, err := s.repositorySource.List()
		if err != nil {
			return err
		}
		for _, repository := range repositories {
			if repository.Builtin {
				skippedCount++
				if s != nil && s.logger != nil {
					s.logger.Debug("Clear plugins skipped builtin repository", "subsystem", "plugin_repos", "repo_id", repository.ID)
				}
				continue
			}
			if err := s.repositorySource.Remove(repository.ID); err != nil {
				if s != nil && s.logger != nil {
					s.logger.Error("Clear plugins aborted", "subsystem", "plugin_repos", "repo_id", repository.ID, "completed_so_far", deletedCount, "error", err)
				}
				return err
			}
			deletedCount++
		}
	}
	if s != nil && s.logger != nil {
		s.logger.Info("Clear plugins completed", "subsystem", "plugins", "deleted", deletedCount, "skipped", skippedCount)
	}
	return nil
}

// ─────────────────────────────────────────────
// Tokens
// ─────────────────────────────────────────────

// ExportTokens returns every router token.
func (s *GormService) ExportTokens(ctx context.Context) ([]*models.RouterToken, error) {
	tokens, err := s.tokenSource.List(ctx)
	if err != nil {
		return nil, err
	}
	if s != nil && s.logger != nil {
		s.logger.Info("Export tokens completed", "subsystem", "tokens", "count", len(tokens))
	}
	return tokens, nil
}

// ImportTokens updates rules for known token IDs, otherwise mints fresh
// tokens. Mint failures return immediately.
func (s *GormService) ImportTokens(ctx context.Context, tokens []*models.RouterToken) error {
	createdCount := 0
	updatedCount := 0
	skippedCount := 0
	for _, tokenRecord := range tokens {
		if tokenRecord == nil {
			skippedCount++
			if s != nil && s.logger != nil {
				s.logger.Warn("Import tokens skipped entry", "subsystem", "tokens", "skipped_count", skippedCount, "reason", "nil_token")
			}
			continue
		}
		if tokenRecord.ID != "" && s.tokenSource.UpdateRules(ctx, tokenRecord.ID, tokenRecord.Rules) == nil {
			updatedCount++
			continue
		}
		if _, err := s.tokenSource.Create(ctx, token.CreateOptions{
			Name:  tokenRecord.Name,
			Rules: tokenRecord.Rules,
		}); err != nil {
			if s != nil && s.logger != nil {
				s.logger.Error("Import tokens aborted", "subsystem", "tokens", "completed_so_far", createdCount+updatedCount, "error", err)
			}
			return err
		}
		createdCount++
	}
	if s != nil && s.logger != nil {
		s.logger.Info("Import tokens completed", "subsystem", "tokens", "created", createdCount, "updated", updatedCount, "skipped", skippedCount)
	}
	return nil
}

// ClearTokens deletes every router token.
func (s *GormService) ClearTokens(ctx context.Context) error {
	tokens, err := s.tokenSource.List(ctx)
	if err != nil {
		return err
	}
	deletedCount := 0
	for _, tokenRecord := range tokens {
		if err := s.tokenSource.Delete(ctx, tokenRecord.ID); err != nil {
			if s != nil && s.logger != nil {
				s.logger.Error("Clear tokens aborted", "subsystem", "tokens", "completed_so_far", deletedCount, "error", err)
			}
			return err
		}
		deletedCount++
	}
	if s != nil && s.logger != nil {
		s.logger.Info("Clear tokens completed", "subsystem", "tokens", "deleted", deletedCount)
	}
	return nil
}

// ─────────────────────────────────────────────
// Proxy subsystem
// ─────────────────────────────────────────────

var errProxyBackendMissing = stdErrors.New("proxy subsystem is not configured")

// ExportProxy packages the proxy switches, manual pools and healthy
// discovered proxies.
func (s *GormService) ExportProxy(ctx context.Context) (*models.ProxyDataBundle, error) {
	if s.proxySource == nil {
		return nil, errProxyBackendMissing
	}
	return s.proxySource.ExportData(ctx)
}

// ImportProxy restores a proxy data bundle.
func (s *GormService) ImportProxy(ctx context.Context, bundle *models.ProxyDataBundle) error {
	if s.proxySource == nil {
		return errProxyBackendMissing
	}
	return s.proxySource.ImportData(ctx, bundle)
}

// ClearProxy removes manual pools and every discovered proxy.
func (s *GormService) ClearProxy(ctx context.Context) error {
	if s.proxySource == nil {
		return errProxyBackendMissing
	}
	return s.proxySource.ClearData(ctx)
}
