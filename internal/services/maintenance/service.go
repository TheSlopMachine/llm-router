// Package maintenance implements the Provider Maintenance & Rotation Service.
//
// Responsibilities:
//   - Periodically scanning all credentials for those that need refresh
//   - Delegating refresh to the appropriate provider backend
//   - Persisting updated credentials via the Credential Pool Service
//
// Design principles:
//   - Flexible: no hardcoded refresh logic — each backend decides its own strategy
//   - Non-blocking: runs in a background goroutine; errors are logged, not fatal
//   - Jitter-free: each check interval is fixed; backends decide when NeedsRefresh is true
package maintenance

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
)

const defaultCheckInterval = 60 * time.Second

// Service runs background maintenance tasks for provider credentials.
type Service struct {
	credSvc     *credential.Service
	providerSvc *provider.Service
	interval    time.Duration
	logger      *slog.Logger
	db          *db.DB

	proxySvc *proxypool.Service
	luaSvc   *luaplugin.Service

	modelInfoSvc modelInfoRefresher

	proxyTickInterval time.Duration
	lastProxyRotate   time.Time
	lastProxyFetch    time.Time
}

// modelInfoRefresher is the slice of modelinfo the maintenance loop needs.
type modelInfoRefresher interface {
	MergedView(ctx context.Context, providerID string) ([]modelinfo.ModelView, error)
}

// SetModelInfoService wires model metadata refresh for providers with
// config.models_auto_sync enabled.
func (s *Service) SetModelInfoService(mi modelInfoRefresher) {
	s.modelInfoSvc = mi
}

// New constructs a new maintenance Service with the default check interval.
func New(credSvc *credential.Service, providerSvc *provider.Service, db *db.DB, logger *slog.Logger) *Service {
	return &Service{
		credSvc:     credSvc,
		providerSvc: providerSvc,
		interval:    defaultCheckInterval,
		logger:      logger,
		db:          db,
	}
}

// SetProxyServices wires the proxy pool and plugin service for periodic
// pool rotation and source fetching.
func (s *Service) SetProxyServices(proxySvc *proxypool.Service, luaSvc *luaplugin.Service) {
	s.proxySvc = proxySvc
	s.luaSvc = luaSvc
}

// SetProxyTickInterval sets the automatic proxy rotation period.
func (s *Service) SetProxyTickInterval(d time.Duration) {
	s.proxyTickInterval = d
}

// WithInterval overrides the check interval (useful for testing).
func (s *Service) WithInterval(d time.Duration) *Service {
	s.interval = d
	return s
}

// Start launches the maintenance loop in a background goroutine.
// It stops when ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	s.logger.Info("maintenance service started", "interval", s.interval)
	go func() {
		// Rotate the proxy pool at once instead of waiting for the first
		// tick: a restarted router re-verifies its pool immediately.
		s.rotateProxyOnce(ctx)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				s.logger.Info("maintenance service stopped")
				return
			case <-ticker.C:
				s.runCycle(ctx)
				s.cleanupAuthFlows()
				s.maintainProxyPool(ctx)
			}
		}
	}()
}

// ─────────────────────────────────────────────
// Maintenance cycle
// ─────────────────────────────────────────────

// runCycle iterates over all credentials and refreshes those that need it.
func (s *Service) runCycle(ctx context.Context) {
	creds, err := s.credSvc.ListAll()
	if err != nil {
		s.logger.Error("maintenance: list credentials failed", "err", err)
		return
	}

	for _, cred := range creds {
		select {
		case <-ctx.Done():
			return
		default:
		}
		s.maybeRefresh(ctx, cred)
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

const proxySourceFetchInterval = time.Hour

// rotateProxyOnce runs one pool rotation pass outside the tick schedule.
func (s *Service) rotateProxyOnce(ctx context.Context) {
	if s.proxySvc == nil {
		return
	}
	s.lastProxyRotate = time.Now()
	if err := s.proxySvc.RotateAll(ctx); err != nil {
		s.logger.Warn("maintenance: startup proxy rotation failed", "err", err)
		return
	}
	s.logger.Info("maintenance: startup proxy rotation completed")
}

// maintainProxyPool rotates the pool on the configured tick and fetches new
// list candidates hourly. Fetching and rotation are independent: the tick
// re-probes pooled proxies, the fetch adds new ones.
func (s *Service) maintainProxyPool(ctx context.Context) {
	if s.proxySvc == nil || s.luaSvc == nil {
		return
	}
	if s.proxyTickInterval > 0 && (s.lastProxyRotate.IsZero() || time.Since(s.lastProxyRotate) >= s.proxyTickInterval) {
		s.lastProxyRotate = time.Now()
		if err := s.proxySvc.RotateAll(ctx); err != nil {
			s.logger.Warn("maintenance: proxy rotation failed", "err", err)
		}
	}
	if !s.lastProxyFetch.IsZero() && time.Since(s.lastProxyFetch) < proxySourceFetchInterval {
		return
	}
	if !s.proxySvc.NeedsSearch() {
		return
	}
	s.lastProxyFetch = time.Now()
	for _, key := range s.luaSvc.ProxySourceKeys() {
		if ctx.Err() != nil {
			return
		}
		s.proxySvc.SetSourceFetching(key)
		candidates, err := s.luaSvc.FetchProxies(ctx, key)
		if err != nil {
			s.proxySvc.SetSourceFailed(key, err)
			s.logger.Warn("maintenance: proxy source refresh failed", "source", key, "err", err)
			continue
		}
		if err := s.proxySvc.AddCandidates(ctx, key, candidates); err != nil {
			// A busy pool means another rotation is already working;
			// not a source failure, so don't stain its status.
			if errors.Is(err, proxypool.ErrBusy) {
				s.proxySvc.SetSourceDone(key, len(candidates))
			} else {
				s.proxySvc.SetSourceFailed(key, err)
			}
			s.logger.Warn("maintenance: proxy pool refresh failed", "source", key, "err", err)
			continue
		}
		s.proxySvc.SetSourceDone(key, len(candidates))
	}
}

// cleanupAuthFlows removes auth flow entries older than 10 minutes.
func (s *Service) cleanupAuthFlows() {
	threshold := time.Now().UTC().Add(-10 * time.Minute)

	type authEntry struct {
		CreatedAt time.Time `json:"created_at"`
	}

	if err := repository.CleanupExpired(s.db, db.BucketAuth, threshold, func(e *authEntry) time.Time {
		return e.CreatedAt
	}); err != nil {
		s.logger.Error("maintenance: cleanup auth flows failed", "err", err)
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
