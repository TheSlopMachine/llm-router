// GORM-backed data management.
//
// GormService mirrors Service against context-taking dependencies so
// export, import, and truncation run unchanged on GORM-backed providers,
// credentials, virtual models, tokens, plugins, repositories, and model
// overrides. It owns no tables and issues no storage statements itself:
// every read and write delegates to the owning service through narrow
// interfaces, so bundle shapes stay identical to the bbolt path. The bbolt
// Service stays live until cutover; this type only orchestrates.
package datamanagement

import (
	"context"
	"fmt"

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
// needs: SQLite list/install/delete. Signatures match the luaplugin SQLite
// methods; storage rows cascade from the plugin delete, so no storage
// statement runs here.
type GormPluginBackend interface {
	ListSQLite(ctx context.Context) ([]*luaplugin.PluginRecord, error)
	InstallSQLite(ctx context.Context, source []byte, origin luaplugin.PluginOrigin) (*luaplugin.PluginRecord, error)
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
// subsystems exactly like the bbolt path.
type GormService struct {
	providerSource   GormProviderBackend
	credentialSource GormCredentialBackend
	virtualSource    GormVirtualBackend
	tokenSource      GormTokenBackend
	pluginSource     GormPluginBackend
	repositorySource GormRepositoryBackend
	modelInfoSource  GormModelInfoBackend
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

// Stats returns the record counts for each managed subsystem. A failing
// subsystem contributes no count, matching the bbolt path: stats stay
// best-effort so one broken subsystem never hides the rest.
func (s *GormService) Stats(ctx context.Context) (SubsystemStats, error) {
	var statistics SubsystemStats
	if providers, err := s.providerSource.List(ctx); err == nil {
		statistics.Providers = len(providers)
	}
	if credentials, err := s.credentialSource.ListAll(ctx); err == nil {
		statistics.Credentials = len(credentials)
	}
	if s.virtualSource != nil {
		if virtualModels, err := s.virtualSource.List(ctx); err == nil {
			statistics.VirtualModels = len(virtualModels)
		}
	}
	if s.tokenSource != nil {
		if tokens, err := s.tokenSource.List(ctx); err == nil {
			statistics.Tokens = len(tokens)
		}
	}
	if s.pluginSource != nil {
		if plugins, err := s.pluginSource.ListSQLite(ctx); err == nil {
			statistics.Plugins = len(plugins)
		}
	}
	if s.repositorySource != nil {
		if repositories, err := s.repositorySource.List(); err == nil {
			statistics.PluginRepos = len(repositories)
		}
	}
	return statistics, nil
}

// ─────────────────────────────────────────────
// Providers
// ─────────────────────────────────────────────

// ExportProviders packages every provider instance with its credentials and
// model overrides. Override list failures yield no overrides for that
// provider, matching the bbolt path: overrides stay optional metadata.
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
			}
		}
		bundles = append(bundles, &ProviderBundle{
			Instance:    providerInstance,
			Credentials: credentialsByProvider[providerInstance.ID],
			Overrides:   overrides,
		})
	}
	return bundles, nil
}

// ImportProviders creates or updates every bundled provider, then its
// credentials and overrides. Update failures return immediately; create
// failures keep the bundle instance so the error surfaces at the credential
// write, matching the bbolt remap discipline.
func (s *GormService) ImportProviders(ctx context.Context, bundles []*ProviderBundle) error {
	for _, bundle := range bundles {
		if bundle == nil || bundle.Instance == nil {
			continue
		}
		instance := bundle.Instance
		if instance.ID == "" {
			continue
		}
		existing, err := s.providerSource.Get(ctx, instance.ID)
		if err == nil && existing != nil {
			if _, err := s.providerSource.Update(ctx, instance.ID, provider.UpdateOptions{
				Name:     instance.Name,
				Config:   instance.Config,
				IconURL:  instance.IconURL,
				Disabled: &instance.Disabled,
			}); err != nil {
				return err
			}
		} else {
			// Create regenerates the ID (dedup suffixes on collision), so
			// credentials and overrides below must follow the created
			// instance, not the bundle's: otherwise they point at an ID
			// that was never stored. On creation failure keep the bundle
			// instance so the error surfaces at the credential write.
			if created, createError := s.providerSource.Create(ctx, provider.CreateOptions{
				Name:      instance.Name,
				TypeKey:   instance.TypeKey,
				Qualifier: instance.Qualifier,
				Config:    instance.Config,
				IconURL:   instance.IconURL,
			}); createError == nil && created != nil {
				instance = created
			}
		}
		for _, credentialItem := range bundle.Credentials {
			if credentialItem == nil {
				continue
			}
			credentialItem.ProviderID = instance.ID
			if credentialItem.ID == "" || s.credentialSource.UpdateDetails(ctx, credentialItem.ID, &credentialItem.Label, &credentialItem.Disabled, credentialItem.Data) != nil {
				if _, err := s.credentialSource.Add(ctx, credential.AddOptions{
					ProviderID: instance.ID,
					Label:      credentialItem.Label,
					Data:       credentialItem.Data,
				}); err != nil {
					return err
				}
			}
		}
		if s.modelInfoSource != nil {
			for _, override := range bundle.Overrides {
				if override == nil || override.Name == "" {
					continue
				}
				override.ProviderID = instance.ID
				if err := s.modelInfoSource.SetOverride(ctx, *override); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ExportProvider packages one provider instance with its credentials and
// overrides. Credential and override list failures yield empty lists,
// matching the bbolt path.
func (s *GormService) ExportProvider(ctx context.Context, identifier string) (*ProviderBundle, error) {
	providerInstance, err := s.providerSource.Get(ctx, identifier)
	if err != nil {
		return nil, err
	}
	var credentials []*models.Credential
	if fetchedCredentials, err := s.credentialSource.ListByProvider(ctx, identifier); err == nil {
		credentials = fetchedCredentials
	}
	var overrides []*models.ModelOverride
	if s.modelInfoSource != nil {
		if fetchedOverrides, err := s.modelInfoSource.ListOverrides(ctx, identifier); err == nil {
			overrides = fetchedOverrides
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
// matching the idempotent bbolt invalidation.
func (s *GormService) PurgeProvider(ctx context.Context, identifier string) error {
	if _, err := s.providerSource.Get(ctx, identifier); err != nil {
		return err
	}
	// 1. Delete provider instance and cascade credentials.
	if err := s.providerSource.Delete(ctx, identifier); err != nil {
		return err
	}
	// 2. Delete model snapshots and overrides through the owning service.
	if s.modelInfoSource != nil {
		if err := s.modelInfoSource.DeleteSnapshot(ctx, identifier); err != nil {
			if !stdErrors.Is(err, apierrors.ErrNotFound) {
				return err
			}
		}
		fetchedOverrides, err := s.modelInfoSource.ListOverrides(ctx, identifier)
		if err != nil {
			return err
		}
		for _, override := range fetchedOverrides {
			if override == nil {
				continue
			}
			if err := s.modelInfoSource.DeleteOverride(ctx, identifier, override.Name); err != nil {
				return err
			}
		}
	}
	// 3. Delete managed virtual models associated with this provider.
	if s.virtualSource != nil {
		virtualModels, err := s.virtualSource.List(ctx)
		if err != nil {
			return err
		}
		for _, virtualModel := range virtualModels {
			if virtualModel.ManagedBy == "" {
				continue
			}
			markerProvider, _, err := virtual.ParseMarker(virtualModel.ManagedBy)
			if err != nil {
				return err
			}
			if markerProvider == identifier {
				if err := s.virtualSource.Delete(ctx, virtualModel.ID); err != nil {
					return err
				}
			}
		}
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
	for _, providerInstance := range providers {
		if err := s.providerSource.Delete(ctx, providerInstance.ID); err != nil {
			return err
		}
	}
	return nil
}

// ─────────────────────────────────────────────
// Virtual Models
// ─────────────────────────────────────────────

// ExportVirtualModels returns every virtual model.
func (s *GormService) ExportVirtualModels(ctx context.Context) ([]*models.VirtualModel, error) {
	return s.virtualSource.List(ctx)
}

// ImportVirtualModels creates or updates every virtual model. Update
// failures return immediately instead of falling through to a duplicate
// create.
func (s *GormService) ImportVirtualModels(ctx context.Context, virtualModels []*models.VirtualModel) error {
	for _, virtualModel := range virtualModels {
		if virtualModel == nil || virtualModel.Name == "" {
			continue
		}
		if virtualModel.ID != "" {
			existing, err := s.virtualSource.Get(ctx, virtualModel.ID)
			if err == nil && existing != nil {
				if err := s.virtualSource.Update(ctx, virtualModel.ID, virtualModel); err != nil {
					return err
				}
				continue
			}
		}
		if err := s.virtualSource.Create(ctx, virtualModel); err != nil {
			return err
		}
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
	for _, virtualModel := range virtualModels {
		if err := s.virtualSource.Delete(ctx, virtualModel.ID); err != nil {
			return err
		}
	}
	return nil
}

// ─────────────────────────────────────────────
// Plugins & Repos
// ─────────────────────────────────────────────

// ExportPlugins packages repository records plus installed plugin records.
// List failures yield empty halves, matching the bbolt path.
func (s *GormService) ExportPlugins(ctx context.Context) (*PluginsExportBundle, error) {
	var bundle PluginsExportBundle
	if s.repositorySource != nil {
		if repositories, err := s.repositorySource.List(); err == nil {
			bundle.Repos = repositories
		}
	}
	if s.pluginSource != nil {
		if plugins, err := s.pluginSource.ListSQLite(ctx); err == nil {
			bundle.Plugins = plugins
		}
	}
	return &bundle, nil
}

// ImportPlugins adds every bundled repository, then installs every bundled
// plugin source. Failures return immediately.
func (s *GormService) ImportPlugins(ctx context.Context, bundle *PluginsExportBundle) error {
	if bundle == nil {
		return nil
	}
	if s.repositorySource != nil {
		for _, repository := range bundle.Repos {
			if repository == nil {
				continue
			}
			targetURL := repository.IndexURL
			if targetURL == "" {
				targetURL = repository.SourceURL
			}
			if targetURL == "" {
				continue
			}
			if _, err := s.repositorySource.AddRepo(ctx, targetURL); err != nil {
				return err
			}
		}
	}
	if s.pluginSource != nil {
		for _, plugin := range bundle.Plugins {
			if plugin == nil || len(plugin.Source) == 0 {
				continue
			}
			if _, err := s.pluginSource.InstallSQLite(ctx, plugin.Source, plugin.Origin); err != nil {
				return err
			}
		}
	}
	return nil
}

// ClearPlugins deletes every plugin and every non-builtin repository.
// Plugin storage, allow host, and version rows cascade from the plugin
// delete in the owning luaplugin service; this package never touches tables
// outside its own directory.
func (s *GormService) ClearPlugins(ctx context.Context) error {
	if s.pluginSource != nil {
		plugins, err := s.pluginSource.ListSQLite(ctx)
		if err != nil {
			return err
		}
		for _, plugin := range plugins {
			if err := s.pluginSource.DeleteSQLite(ctx, plugin.ID); err != nil {
				return err
			}
		}
	}
	if s.repositorySource != nil {
		repositories, err := s.repositorySource.List()
		if err != nil {
			return err
		}
		for _, repository := range repositories {
			if repository.Builtin {
				continue
			}
			if err := s.repositorySource.Remove(repository.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// ─────────────────────────────────────────────
// Tokens
// ─────────────────────────────────────────────

// ExportTokens returns every router token.
func (s *GormService) ExportTokens(ctx context.Context) ([]*models.RouterToken, error) {
	return s.tokenSource.List(ctx)
}

// ImportTokens updates rules for known token IDs, otherwise mints fresh
// tokens. Mint failures return immediately.
func (s *GormService) ImportTokens(ctx context.Context, tokens []*models.RouterToken) error {
	for _, tokenRecord := range tokens {
		if tokenRecord == nil {
			continue
		}
		if tokenRecord.ID != "" && s.tokenSource.UpdateRules(ctx, tokenRecord.ID, tokenRecord.Rules) == nil {
			continue
		}
		if _, err := s.tokenSource.Create(ctx, token.CreateOptions{
			Name:  tokenRecord.Name,
			Rules: tokenRecord.Rules,
		}); err != nil {
			return err
		}
	}
	return nil
}

// ClearTokens deletes every router token.
func (s *GormService) ClearTokens(ctx context.Context) error {
	tokens, err := s.tokenSource.List(ctx)
	if err != nil {
		return err
	}
	for _, tokenRecord := range tokens {
		if err := s.tokenSource.Delete(ctx, tokenRecord.ID); err != nil {
			return err
		}
	}
	return nil
}
