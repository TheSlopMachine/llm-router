package datamanagement

import (
	"context"
	"fmt"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/pluginrepo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
	bolt "go.etcd.io/bbolt"
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
}

// Service provides export, import and truncation operations for router subsystems.
type Service struct {
	database          *db.DB
	providerService   *provider.Service
	credentialService *credential.Service
	virtualService    *virtual.Service
	tokenService      *token.Service
	luaService        *luaplugin.Service
	repositoryService *pluginrepo.Service
	modelInfoService  *modelinfo.Service
}

func New(
	database *db.DB,
	providerService *provider.Service,
	credentialService *credential.Service,
	virtualService *virtual.Service,
	tokenService *token.Service,
	luaService *luaplugin.Service,
	repositoryService *pluginrepo.Service,
	modelInfoService *modelinfo.Service,
) *Service {
	return &Service{
		database:          database,
		providerService:   providerService,
		credentialService: credentialService,
		virtualService:    virtualService,
		tokenService:      tokenService,
		luaService:        luaService,
		repositoryService: repositoryService,
		modelInfoService:  modelInfoService,
	}
}

// Stats returns the record counts for each managed subsystem.
func (s *Service) Stats() (SubsystemStats, error) {
	var statistics SubsystemStats
	providers, err := s.providerService.List()
	if err == nil {
		statistics.Providers = len(providers)
	}
	credentials, err := s.credentialService.ListAll()
	if err == nil {
		statistics.Credentials = len(credentials)
	}
	virtualModels, err := s.virtualService.List()
	if err == nil {
		statistics.VirtualModels = len(virtualModels)
	}
	tokens, err := s.tokenService.List()
	if err == nil {
		statistics.Tokens = len(tokens)
	}
	if s.luaService != nil {
		plugins, err := s.luaService.List()
		if err == nil {
			statistics.Plugins = len(plugins)
		}
	}
	if s.repositoryService != nil {
		repositories, err := s.repositoryService.List()
		if err == nil {
			statistics.PluginRepos = len(repositories)
		}
	}
	return statistics, nil
}

// ─────────────────────────────────────────────
// Providers
// ─────────────────────────────────────────────

func (s *Service) ExportProviders() ([]*ProviderBundle, error) {
	providers, err := s.providerService.List()
	if err != nil {
		return nil, err
	}
	credentials, err := s.credentialService.ListAll()
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
		if s.modelInfoService != nil {
			if fetchedOverrides, err := s.modelInfoService.ListOverrides(providerInstance.ID); err == nil {
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

func (s *Service) ImportProviders(bundles []*ProviderBundle) error {
	for _, bundle := range bundles {
		if bundle == nil || bundle.Instance == nil {
			continue
		}
		instance := bundle.Instance
		if instance.ID == "" {
			continue
		}
		existing, err := s.providerService.Get(instance.ID)
		if err == nil && existing != nil {
			_, _ = s.providerService.Update(instance.ID, provider.UpdateOptions{
				Name:     instance.Name,
				Config:   instance.Config,
				IconURL:  instance.IconURL,
				Disabled: &instance.Disabled,
			})
		} else {
			// Create regenerates the ID (dedup suffixes on collision), so
			// credentials and overrides below must follow the created
			// instance, not the bundle's: otherwise they point at an ID
			// that was never stored. On creation failure keep the bundle
			// instance so the error surfaces at the credential write.
			if created, createError := s.providerService.Create(provider.CreateOptions{
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
			if credentialItem.ID == "" || s.credentialService.UpdateDetails(credentialItem.ID, &credentialItem.Label, &credentialItem.Disabled, credentialItem.Data) != nil {
				_, _ = s.credentialService.Add(credential.AddOptions{
					ProviderID: instance.ID,
					Label:      credentialItem.Label,
					Data:       credentialItem.Data,
				})
			}
		}
		if s.modelInfoService != nil {
			for _, override := range bundle.Overrides {
				if override == nil || override.Name == "" {
					continue
				}
				override.ProviderID = instance.ID
				_ = s.modelInfoService.SetOverride(*override)
			}
		}
	}
	return nil
}

func (s *Service) ExportProvider(id string) (*ProviderBundle, error) {
	providerInstance, err := s.providerService.Get(id)
	if err != nil {
		return nil, err
	}
	credentials, err := s.credentialService.ListByProvider(id)
	if err != nil {
		credentials = nil
	}
	var overrides []*models.ModelOverride
	if s.modelInfoService != nil {
		if fetchedOverrides, err := s.modelInfoService.ListOverrides(id); err == nil {
			overrides = fetchedOverrides
		}
	}
	return &ProviderBundle{
		Instance:    providerInstance,
		Credentials: credentials,
		Overrides:   overrides,
	}, nil
}

func (s *Service) ImportProvider(bundle *ProviderBundle) error {
	if bundle == nil || bundle.Instance == nil {
		return fmt.Errorf("empty provider bundle")
	}
	return s.ImportProviders([]*ProviderBundle{bundle})
}

func (s *Service) PurgeProvider(id string) error {
	if _, err := s.providerService.Get(id); err != nil {
		return err
	}
	// 1. Delete provider instance and cascade credentials
	_ = s.providerService.Delete(id)

	// 2. Delete model overrides and model infos through the owning
	// service: override keys are providerID/modelName inside modelinfo,
	// so listing by provider value removes exactly this provider's rows
	// without reimplementing the key format here.
	if s.modelInfoService != nil {
		_ = s.modelInfoService.InvalidateProvider(id)
		if fetchedOverrides, err := s.modelInfoService.ListOverrides(id); err == nil {
			for _, override := range fetchedOverrides {
				if override != nil {
					_ = s.modelInfoService.DeleteOverride(id, override.Name)
				}
			}
		}
	}

	// 3. Delete managed virtual models associated with this provider
	if s.virtualService != nil {
		if virtualModels, err := s.virtualService.List(); err == nil {
			for _, virtualModel := range virtualModels {
				if virtualModel.ManagedBy != "" {
					markerProvider, _, err := virtual.ParseMarker(virtualModel.ManagedBy)
					if err == nil && markerProvider == id {
						_ = s.virtualService.Delete(virtualModel.ID)
					}
				}
			}
		}
	}

	return nil
}

func (s *Service) ClearProviders() error {
	providers, err := s.providerService.List()
	if err != nil {
		return err
	}
	for _, providerInstance := range providers {
		_ = s.providerService.Delete(providerInstance.ID)
	}
	return nil
}

// ─────────────────────────────────────────────
// Virtual Models
// ─────────────────────────────────────────────

func (s *Service) ExportVirtualModels() ([]*models.VirtualModel, error) {
	return s.virtualService.List()
}

func (s *Service) ImportVirtualModels(virtualModels []*models.VirtualModel) error {
	for _, virtualModel := range virtualModels {
		if virtualModel == nil || virtualModel.Name == "" {
			continue
		}
		if virtualModel.ID != "" {
			existing, err := s.virtualService.Get(virtualModel.ID)
			if err == nil && existing != nil {
				_ = s.virtualService.Update(virtualModel.ID, virtualModel)
				continue
			}
		}
		_ = s.virtualService.Create(virtualModel)
	}
	return nil
}

func (s *Service) ClearVirtualModels() error {
	virtualModels, err := s.virtualService.List()
	if err != nil {
		return err
	}
	for _, virtualModel := range virtualModels {
		_ = s.virtualService.Delete(virtualModel.ID)
	}
	return nil
}

// ─────────────────────────────────────────────
// Plugins & Repos
// ─────────────────────────────────────────────

type PluginsExportBundle struct {
	Repos   []*pluginrepo.RepoRecord  `json:"repos"`
	Plugins []*luaplugin.PluginRecord `json:"plugins"`
}

func (s *Service) ExportPlugins() (*PluginsExportBundle, error) {
	var bundle PluginsExportBundle
	if s.repositoryService != nil {
		repositories, err := s.repositoryService.List()
		if err == nil {
			bundle.Repos = repositories
		}
	}
	if s.luaService != nil {
		plugins, err := s.luaService.List()
		if err == nil {
			bundle.Plugins = plugins
		}
	}
	return &bundle, nil
}

func (s *Service) ImportPlugins(ctx context.Context, bundle *PluginsExportBundle) error {
	if bundle == nil {
		return nil
	}
	if s.repositoryService != nil {
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
			_, _ = s.repositoryService.AddRepo(ctx, targetURL)
		}
	}
	if s.luaService != nil {
		for _, plugin := range bundle.Plugins {
			if plugin == nil || len(plugin.Source) == 0 {
				continue
			}
			_, _ = s.luaService.Install(plugin.Source, plugin.Origin)
		}
	}
	return nil
}

func (s *Service) ClearPlugins() error {
	if s.luaService != nil {
		plugins, err := s.luaService.List()
		if err == nil {
			for _, plugin := range plugins {
				_ = s.luaService.Delete(plugin.ID)
			}
		}
	}
	if s.repositoryService != nil {
		repositories, err := s.repositoryService.List()
		if err == nil {
			for _, repository := range repositories {
				if repository.Builtin {
					continue
				}
				_ = s.repositoryService.Remove(repository.ID)
			}
		}
	}
	_ = s.database.Update(func(tx *bolt.Tx) error {
		_ = tx.DeleteBucket(db.BucketPluginStorage)
		_, _ = tx.CreateBucketIfNotExists(db.BucketPluginStorage)
		return nil
	})
	return nil
}

// ─────────────────────────────────────────────
// Tokens
// ─────────────────────────────────────────────

func (s *Service) ExportTokens() ([]*models.RouterToken, error) {
	return s.tokenService.List()
}

func (s *Service) ImportTokens(tokens []*models.RouterToken) error {
	for _, tokenRecord := range tokens {
		if tokenRecord == nil {
			continue
		}
		if tokenRecord.ID != "" && s.tokenService.UpdateRules(tokenRecord.ID, tokenRecord.Rules) == nil {
			continue
		}
		_, _ = s.tokenService.Create(token.CreateOptions{
			Name:  tokenRecord.Name,
			Rules: tokenRecord.Rules,
		})
	}
	return nil
}

func (s *Service) ClearTokens() error {
	tokens, err := s.tokenService.List()
	if err != nil {
		return err
	}
	for _, tokenRecord := range tokens {
		_ = s.tokenService.Delete(tokenRecord.ID)
	}
	return nil
}
