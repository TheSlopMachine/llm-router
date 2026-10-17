package proxypool

import (
	"context"
	"fmt"
	"sort"
	"strings"

	proxypoollib "github.com/TheSlopMachine/proxypool"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// importSourceName tags proxies that arrived through a data import.
const importSourceName = "import"

// takeImported hands the pending imported proxies to the next ingest.
func (s *Service) takeImported() []proxypoollib.TaggedURL {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	pending := s.imported
	s.imported = nil
	return pending
}

// DataCounts reports the number of manual pools and healthy discovered
// proxies for the data management overview.
func (s *Service) DataCounts(ctx context.Context) (pools, proxies int, err error) {
	custom, err := s.ListCustomPools(ctx)
	if err != nil {
		return 0, 0, err
	}
	return len(custom), len(s.aliveStates()), nil
}

// aliveStates lists healthy cached proxies straight from the cache, so the
// result stays correct while discovery is switched off and the library index
// was never built.
func (s *Service) aliveStates() []proxypoollib.ProxyState {
	var alive []proxypoollib.ProxyState
	for _, state := range s.cache.All() {
		if !state.IsDead && !state.Suspect {
			alive = append(alive, state)
		}
	}
	return alive
}

// ExportData packages the switches, the manual pools and the healthy
// discovered proxies.
func (s *Service) ExportData(ctx context.Context) (*models.ProxyDataBundle, error) {
	pools, err := s.ListCustomPools(ctx)
	if err != nil {
		return nil, err
	}
	bundle := &models.ProxyDataBundle{
		Version: models.ProxyDataVersion,
		Pools:   make([]models.CustomProxyPool, 0, len(pools)),
		Proxies: []models.ProxyDataEntry{},
	}
	s.settingsMu.RLock()
	bundle.Settings = &models.ProxyDataSetting{Enabled: !s.disabled, PoolEnabled: !s.poolDisabled, MaxChecks: s.maxChecks}
	s.settingsMu.RUnlock()
	for _, pool := range pools {
		bundle.Pools = append(bundle.Pools, *pool)
	}
	alive := s.aliveStates()
	sort.Slice(alive, func(left, right int) bool { return alive[left].URL < alive[right].URL })
	for _, state := range alive {
		bundle.Proxies = append(bundle.Proxies, models.ProxyDataEntry{URL: state.URL, Country: state.Location})
	}
	s.currentLogger().Info("Exporting proxy data", "pools", len(bundle.Pools), "proxies", len(bundle.Proxies))
	return bundle, nil
}

// ImportData restores manual pools, the switches and the check cap, and
// queues the exported proxies for a fresh check. Existing pools with the same
// ID are replaced. Proxies are never trusted as healthy: they join the
// candidate queue on the next ingest, which runs now when discovery is on.
func (s *Service) ImportData(ctx context.Context, bundle *models.ProxyDataBundle) error {
	if bundle == nil {
		return fmt.Errorf("proxy data bundle is required")
	}
	if bundle.Version != models.ProxyDataVersion {
		return fmt.Errorf("unsupported proxy data version %d, expected %d", bundle.Version, models.ProxyDataVersion)
	}
	if bundle.Settings != nil && (bundle.Settings.MaxChecks < 0 || bundle.Settings.MaxChecks > models.MaxProxyChecks) {
		return fmt.Errorf("proxy max checks must be between 0 and %d", models.MaxProxyChecks)
	}
	for index := range bundle.Pools {
		pool := bundle.Pools[index]
		if strings.EqualFold(strings.TrimSpace(pool.ID), builtinFreePoolID) || strings.EqualFold(strings.TrimSpace(pool.Name), builtinFreePoolID) {
			continue
		}
		if _, err := s.SaveCustomPool(ctx, &pool); err != nil {
			return fmt.Errorf("import pool %q: %w", pool.Name, err)
		}
	}
	if bundle.Settings != nil {
		if err := s.persistSettings(ctx, *bundle.Settings); err != nil {
			return err
		}
		enabled, poolEnabled, maxChecks := bundle.Settings.Enabled, bundle.Settings.PoolEnabled, bundle.Settings.MaxChecks
		if err := s.ApplyConfiguration(models.RouterConfiguration{ProxyEnabled: &enabled, ProxyPoolEnabled: &poolEnabled, ProxyMaxChecks: &maxChecks}); err != nil {
			return err
		}
	}
	accepted := make([]proxypoollib.TaggedURL, 0, len(bundle.Proxies))
	for _, entry := range bundle.Proxies {
		validated, ok := candidateURL(Candidate{URL: strings.TrimSpace(entry.URL), Country: entry.Country})
		if !ok {
			continue
		}
		accepted = append(accepted, proxypoollib.TaggedURL{URL: validated, Source: importSourceName, Country: strings.ToUpper(strings.TrimSpace(entry.Country))})
	}
	if len(accepted) > 0 {
		s.settingsMu.Lock()
		s.imported = append(s.imported, accepted...)
		s.settingsMu.Unlock()
		s.RequestIngest()
	}
	s.currentLogger().Info("Imported proxy data", "pools", len(bundle.Pools), "proxies", len(accepted))
	return nil
}

// persistSettings stores imported switches and cap in the instance row.
func (s *Service) persistSettings(ctx context.Context, setting models.ProxyDataSetting) error {
	database, err := s.requireHandle()
	if err != nil {
		return err
	}
	result := database.Handle.WithContext(ctx).Model(&record.InstanceSetting{}).Where("id = ?", 1).Updates(map[string]any{
		"proxy_max_checks":    setting.MaxChecks,
		"proxy_disabled":      !setting.Enabled,
		"proxy_pool_disabled": !setting.PoolEnabled,
	})
	if result.Error != nil {
		return fmt.Errorf("save proxy settings: %w", result.Error)
	}
	return nil
}

// ClearData deletes every manual pool and every discovered proxy with its
// queues. The switches and the check cap stay. Pools that providers still
// reference survive and are reported in the error.
func (s *Service) ClearData(ctx context.Context) error {
	s.activityMu.Lock()
	defer s.activityMu.Unlock()

	s.mu.Lock()
	running := s.running
	rootCtx := s.rootCtx
	s.mu.Unlock()
	restart := running && rootCtx != nil && s.discoveryEnabled()
	if restart {
		s.pool.Stop()
	}
	s.cache.Clear()
	s.pool.Reset()
	s.settingsMu.Lock()
	s.imported = nil
	s.settingsMu.Unlock()
	if restart {
		s.pool.Start(rootCtx)
	}

	pools, err := s.ListCustomPools(ctx)
	if err != nil {
		return err
	}
	var inUse []string
	for _, pool := range pools {
		if err := s.DeleteCustomPool(ctx, pool.ID); err != nil {
			inUse = append(inUse, pool.Name)
		}
	}
	s.currentLogger().Info("Cleared proxy data", "pools_deleted", len(pools)-len(inUse), "pools_kept", len(inUse))
	if len(inUse) > 0 {
		return fmt.Errorf("proxy pools still used by providers were kept: %s", strings.Join(inUse, ", "))
	}
	return nil
}
