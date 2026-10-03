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
	db           *db.DB
	providerSvc  *provider.Service
	credSvc      *credential.Service
	virtualSvc   *virtual.Service
	tokenSvc     *token.Service
	luaSvc       *luaplugin.Service
	repoSvc      *pluginrepo.Service
	modelInfoSvc *modelinfo.Service
}

func New(
	database *db.DB,
	providerSvc *provider.Service,
	credSvc *credential.Service,
	virtualSvc *virtual.Service,
	tokenSvc *token.Service,
	luaSvc *luaplugin.Service,
	repoSvc *pluginrepo.Service,
	modelInfoSvc *modelinfo.Service,
) *Service {
	return &Service{
		db:           database,
		providerSvc:  providerSvc,
		credSvc:      credSvc,
		virtualSvc:   virtualSvc,
		tokenSvc:     tokenSvc,
		luaSvc:       luaSvc,
		repoSvc:      repoSvc,
		modelInfoSvc: modelInfoSvc,
	}
}

// Stats returns the record counts for each managed subsystem.
func (s *Service) Stats() (SubsystemStats, error) {
	var st SubsystemStats
	providers, err := s.providerSvc.List()
	if err == nil {
		st.Providers = len(providers)
	}
	creds, err := s.credSvc.ListAll()
	if err == nil {
		st.Credentials = len(creds)
	}
	vms, err := s.virtualSvc.List()
	if err == nil {
		st.VirtualModels = len(vms)
	}
	tokens, err := s.tokenSvc.List()
	if err == nil {
		st.Tokens = len(tokens)
	}
	if s.luaSvc != nil {
		plugins, err := s.luaSvc.List()
		if err == nil {
			st.Plugins = len(plugins)
		}
	}
	if s.repoSvc != nil {
		repos, err := s.repoSvc.List()
		if err == nil {
			st.PluginRepos = len(repos)
		}
	}
	return st, nil
}

// ─────────────────────────────────────────────
// Providers
// ─────────────────────────────────────────────

func (s *Service) ExportProviders() ([]*ProviderBundle, error) {
	providers, err := s.providerSvc.List()
	if err != nil {
		return nil, err
	}
	creds, err := s.credSvc.ListAll()
	if err != nil {
		return nil, err
	}
	credsByProvider := map[string][]*models.Credential{}
	for _, c := range creds {
		credsByProvider[c.ProviderID] = append(credsByProvider[c.ProviderID], c)
	}

	bundles := make([]*ProviderBundle, 0, len(providers))
	for _, p := range providers {
		var overrides []*models.ModelOverride
		if s.modelInfoSvc != nil {
			if ovs, err := s.modelInfoSvc.ListOverrides(p.ID); err == nil {
				overrides = ovs
			}
		}
		bundles = append(bundles, &ProviderBundle{
			Instance:    p,
			Credentials: credsByProvider[p.ID],
			Overrides:   overrides,
		})
	}
	return bundles, nil
}

func (s *Service) ImportProviders(bundles []*ProviderBundle) error {
	for _, b := range bundles {
		if b == nil || b.Instance == nil {
			continue
		}
		inst := b.Instance
		if inst.ID == "" {
			continue
		}
		existing, err := s.providerSvc.Get(inst.ID)
		if err == nil && existing != nil {
			_, _ = s.providerSvc.Update(inst.ID, provider.UpdateOptions{
				Name:     inst.Name,
				Config:   inst.Config,
				IconURL:  inst.IconURL,
				Disabled: &inst.Disabled,
			})
		} else {
			// Create regenerates the ID (dedup suffixes on collision), so
			// credentials and overrides below must follow the created
			// instance, not the bundle's: otherwise they point at an ID
			// that was never stored. On creation failure keep the bundle
			// instance so the error surfaces at the credential write.
			if created, cerr := s.providerSvc.Create(provider.CreateOptions{
				Name:      inst.Name,
				TypeKey:   inst.TypeKey,
				Qualifier: inst.Qualifier,
				Config:    inst.Config,
				IconURL:   inst.IconURL,
			}); cerr == nil && created != nil {
				inst = created
			}
		}
		for _, c := range b.Credentials {
			if c == nil {
				continue
			}
			c.ProviderID = inst.ID
			if c.ID == "" || s.credSvc.UpdateDetails(c.ID, &c.Label, &c.Disabled, c.Data) != nil {
				_, _ = s.credSvc.Add(credential.AddOptions{
					ProviderID: inst.ID,
					Label:      c.Label,
					Data:       c.Data,
				})
			}
		}
		if s.modelInfoSvc != nil {
			for _, ov := range b.Overrides {
				if ov == nil || ov.Name == "" {
					continue
				}
				ov.ProviderID = inst.ID
				_ = s.modelInfoSvc.SetOverride(*ov)
			}
		}
	}
	return nil
}

func (s *Service) ExportProvider(id string) (*ProviderBundle, error) {
	p, err := s.providerSvc.Get(id)
	if err != nil {
		return nil, err
	}
	creds, err := s.credSvc.ListByProvider(id)
	if err != nil {
		creds = nil
	}
	var overrides []*models.ModelOverride
	if s.modelInfoSvc != nil {
		if ovs, err := s.modelInfoSvc.ListOverrides(id); err == nil {
			overrides = ovs
		}
	}
	return &ProviderBundle{
		Instance:    p,
		Credentials: creds,
		Overrides:   overrides,
	}, nil
}

func (s *Service) ImportProvider(b *ProviderBundle) error {
	if b == nil || b.Instance == nil {
		return fmt.Errorf("empty provider bundle")
	}
	return s.ImportProviders([]*ProviderBundle{b})
}

func (s *Service) PurgeProvider(id string) error {
	if _, err := s.providerSvc.Get(id); err != nil {
		return err
	}
	// 1. Delete provider instance and cascade credentials
	_ = s.providerSvc.Delete(id)

	// 2. Delete model overrides and model infos through the owning
	// service: override keys are providerID/modelName inside modelinfo,
	// so listing by provider value removes exactly this provider's rows
	// without reimplementing the key format here.
	if s.modelInfoSvc != nil {
		_ = s.modelInfoSvc.InvalidateProvider(id)
		if ovs, err := s.modelInfoSvc.ListOverrides(id); err == nil {
			for _, ov := range ovs {
				if ov != nil {
					_ = s.modelInfoSvc.DeleteOverride(id, ov.Name)
				}
			}
		}
	}

	// 3. Delete managed virtual models associated with this provider
	if s.virtualSvc != nil {
		if vms, err := s.virtualSvc.List(); err == nil {
			for _, vm := range vms {
				if vm.ManagedBy != "" {
					markerProvider, _, err := virtual.ParseMarker(vm.ManagedBy)
					if err == nil && markerProvider == id {
						_ = s.virtualSvc.Delete(vm.ID)
					}
				}
			}
		}
	}

	return nil
}

func (s *Service) ClearProviders() error {
	providers, err := s.providerSvc.List()
	if err != nil {
		return err
	}
	for _, p := range providers {
		_ = s.providerSvc.Delete(p.ID)
	}
	return nil
}

// ─────────────────────────────────────────────
// Virtual Models
// ─────────────────────────────────────────────

func (s *Service) ExportVirtualModels() ([]*models.VirtualModel, error) {
	return s.virtualSvc.List()
}

func (s *Service) ImportVirtualModels(vms []*models.VirtualModel) error {
	for _, vm := range vms {
		if vm == nil || vm.Name == "" {
			continue
		}
		if vm.ID != "" {
			existing, err := s.virtualSvc.Get(vm.ID)
			if err == nil && existing != nil {
				_ = s.virtualSvc.Update(vm.ID, vm)
				continue
			}
		}
		_ = s.virtualSvc.Create(vm)
	}
	return nil
}

func (s *Service) ClearVirtualModels() error {
	vms, err := s.virtualSvc.List()
	if err != nil {
		return err
	}
	for _, vm := range vms {
		_ = s.virtualSvc.Delete(vm.ID)
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
	if s.repoSvc != nil {
		repos, err := s.repoSvc.List()
		if err == nil {
			bundle.Repos = repos
		}
	}
	if s.luaSvc != nil {
		plugins, err := s.luaSvc.List()
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
	if s.repoSvc != nil {
		for _, repo := range bundle.Repos {
			if repo == nil {
				continue
			}
			targetURL := repo.IndexURL
			if targetURL == "" {
				targetURL = repo.SourceURL
			}
			if targetURL == "" {
				continue
			}
			_, _ = s.repoSvc.AddRepo(ctx, targetURL)
		}
	}
	if s.luaSvc != nil {
		for _, p := range bundle.Plugins {
			if p == nil || len(p.Source) == 0 {
				continue
			}
			_, _ = s.luaSvc.Install(p.Source, p.Origin)
		}
	}
	return nil
}

func (s *Service) ClearPlugins() error {
	if s.luaSvc != nil {
		plugins, err := s.luaSvc.List()
		if err == nil {
			for _, p := range plugins {
				_ = s.luaSvc.Delete(p.ID)
			}
		}
	}
	if s.repoSvc != nil {
		repos, err := s.repoSvc.List()
		if err == nil {
			for _, r := range repos {
				if r.Builtin {
					continue
				}
				_ = s.repoSvc.Remove(r.ID)
			}
		}
	}
	_ = s.db.Update(func(tx *bolt.Tx) error {
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
	return s.tokenSvc.List()
}

func (s *Service) ImportTokens(tokens []*models.RouterToken) error {
	for _, t := range tokens {
		if t == nil {
			continue
		}
		if t.ID != "" && s.tokenSvc.UpdateRules(t.ID, t.Rules) == nil {
			continue
		}
		_, _ = s.tokenSvc.Create(token.CreateOptions{
			Name:  t.Name,
			Rules: t.Rules,
		})
	}
	return nil
}

func (s *Service) ClearTokens() error {
	tokens, err := s.tokenSvc.List()
	if err != nil {
		return err
	}
	for _, t := range tokens {
		_ = s.tokenSvc.Delete(t.ID)
	}
	return nil
}
