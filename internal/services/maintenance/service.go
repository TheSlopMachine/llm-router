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

	lastProxyRefresh time.Time
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
// pool rotation (list refresh + health checks).
func (s *Service) SetProxyServices(proxySvc *proxypool.Service, luaSvc *luaplugin.Service) {
	s.proxySvc = proxySvc
	s.luaSvc = luaSvc
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
}

const proxyPoolRefreshInterval = time.Hour

// maintainProxyPool refreshes list-sourced proxies and probes the pool
// hourly; checks run on the regular ticker when a refresh is due.
func (s *Service) maintainProxyPool(ctx context.Context) {
	if s.proxySvc == nil || s.luaSvc == nil {
		return
	}
	if time.Since(s.lastProxyRefresh) < proxyPoolRefreshInterval {
		return
	}
	s.lastProxyRefresh = time.Now()
	for _, key := range s.luaSvc.ProxySourceKeys() {
		if ctx.Err() != nil {
			return
		}
		candidates, err := s.luaSvc.FetchProxies(ctx, key)
		if err != nil {
			s.logger.Warn("maintenance: proxy source refresh failed", "source", key, "err", err)
			continue
		}
		if _, err := s.proxySvc.SyncFromSource(key, candidates); err != nil {
			s.logger.Warn("maintenance: proxy pool sync failed", "source", key, "err", err)
		}
	}
	s.proxySvc.CheckAll(ctx)
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
