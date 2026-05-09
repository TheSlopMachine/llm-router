// Package maintenance implements the Provider Maintenance & Rotation Service.
//
// Responsibilities:
//   - Periodically scanning all credentials for those that need refresh
//   - Delegating refresh to the appropriate Provider Adapter
//   - Persisting updated credentials via the Credential Pool Service
//
// Design principles:
//   - Flexible: no hardcoded refresh logic — each adapter decides its own strategy
//   - Non-blocking: runs in a background goroutine; errors are logged, not fatal
//   - Jitter-free: each check interval is fixed; adapters decide when NeedsRefresh is true
package maintenance

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

const defaultCheckInterval = 60 * time.Second

// Service runs background maintenance tasks for provider credentials.
type Service struct {
	credSvc     *credential.Service
	providerSvc *provider.Service
	interval    time.Duration
	logger      *slog.Logger
	db          *db.DB
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

// maybeRefresh checks a single credential and refreshes it if the adapter
// reports it needs refreshing.
func (s *Service) maybeRefresh(ctx context.Context, cred *models.Credential) {
	adapter, p, err := provider.ResolveAdapter(s.providerSvc, cred.ProviderID)
	if err != nil {
		s.logger.Warn("maintenance: provider resolution failed",
			"credential_id", cred.ID, "provider_id", cred.ProviderID, "err", err)
		return
	}

	if !adapter.NeedsRefresh(cred.ToSDK()) {
		return
	}

	s.logger.Info("maintenance: refreshing credential",
		"credential_id", cred.ID, "provider", p.Name)

	updated, err := adapter.RefreshCredential(ctx, cred.ToSDK())
	if err != nil {
		if errors.Is(err, provider.ErrNoRefreshNeeded) {
			// Static credentials (e.g. API keys) — nothing to do
			return
		}
		s.logger.Error("maintenance: refresh failed",
			"credential_id", cred.ID, "provider", p.Name, "err", err)
		return
	}

	if err := s.credSvc.Update(cred.ID, updated.Data, nil); err != nil {
		s.logger.Error("maintenance: persist refreshed credential failed",
			"credential_id", cred.ID, "err", err)
		return
	}

	s.logger.Info("maintenance: credential refreshed successfully",
		"credential_id", cred.ID, "provider", p.Name)
}

