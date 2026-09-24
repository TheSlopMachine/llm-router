package maintenance

import (
	"context"
	"errors"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

// runCycle refreshes stale credentials, then syncs opted-in model lists.
// A credential-list failure never skips the model sync: the jobs are
// independent and share only the tick.
func (s *Service) runCycle(ctx context.Context) {
	if creds, err := s.credSvc.ListAll(); err != nil {
		s.logger.Error("maintenance: list credentials failed", "err", err)
	} else {
		for _, cred := range creds {
			select {
			case <-ctx.Done():
				return
			default:
			}
			s.maybeRefresh(ctx, cred)
		}
	}

	s.syncProviderModels(ctx)
}

// syncProviderModels warms the model metadata cache for providers that opted
// into automatic sync (config.models_auto_sync). The MergedView TTL (1h)
// throttles actual upstream calls; disabled providers are skipped.
func (s *Service) syncProviderModels(ctx context.Context) {
	if s.modelInfoSvc == nil {
		return
	}
	providers, err := s.providerSvc.List()
	if err != nil {
		s.logger.Warn("maintenance: list providers for model sync failed", "err", err)
		return
	}
	for _, p := range providers {
		if ctx.Err() != nil {
			return
		}
		if p.Disabled {
			continue
		}
		enabled, _ := p.Config["models_auto_sync"].(bool)
		if !enabled {
			continue
		}
		if _, err := s.modelInfoSvc.MergedView(ctx, p.ID); err != nil {
			s.logger.Warn("maintenance: model auto-sync failed", "provider_id", p.ID, "err", err)
		}
	}
}

// maybeRefresh checks a single credential and refreshes it when the backend
// reports it needs refreshing. Backends without refresh handlers are skipped.
func (s *Service) maybeRefresh(ctx context.Context, cred *models.Credential) {
	resolved, err := provider.Resolve(s.providerSvc, cred.ProviderID)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.logger.Debug("maintenance: orphan credential (provider deleted)",
				"credential_id", cred.ID, "provider_id", cred.ProviderID, "err", err)
		} else {
			s.logger.Warn("maintenance: provider resolution failed",
				"credential_id", cred.ID, "provider_id", cred.ProviderID, "err", err)
		}
		return
	}

	needs, err := s.needsRefresh(resolved, cred)
	if err != nil {
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return
		}
		s.logger.Warn("maintenance: needs-refresh check failed",
			"credential_id", cred.ID, "provider_id", cred.ProviderID, "err", err)
		return
	}
	if !needs {
		return
	}

	s.logger.Info("maintenance: refreshing credential",
		"credential_id", cred.ID, "provider", resolved.Instance.Name)

	data, err := s.refresh(ctx, resolved, cred)
	if err != nil {
		if errors.Is(err, luaplugin.ErrHandlerNotFound) || errors.Is(err, provider.ErrNotRefreshable) {
			return
		}
		s.logger.Error("maintenance: refresh failed",
			"credential_id", cred.ID, "provider", resolved.Instance.Name, "err", err)
		return
	}

	if err := s.credSvc.Update(cred.ID, data, nil); err != nil {
		s.logger.Error("maintenance: persist refreshed credential failed",
			"credential_id", cred.ID, "err", err)
		return
	}

	s.logger.Info("maintenance: credential refreshed successfully",
		"credential_id", cred.ID, "provider", resolved.Instance.Name)
}

func (s *Service) needsRefresh(resolved *provider.Resolved, cred *models.Credential) (bool, error) {
	if resolved.IsLua() {
		return s.providerSvc.LuaService().NeedsRefresh(resolved.Instance.TypeKey, cred)
	}
	return resolved.Go.NeedsRefresh(cred), nil
}

func (s *Service) refresh(ctx context.Context, resolved *provider.Resolved, cred *models.Credential) (map[string]any, error) {
	if resolved.IsLua() {
		return s.providerSvc.LuaService().RefreshCredential(ctx, resolved.Instance.TypeKey, cred)
	}
	return resolved.Go.RefreshCredential(ctx, cred)
}
