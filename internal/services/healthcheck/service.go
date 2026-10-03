// Package healthcheck owns failure-triggered account health verification.
//
// A failed attempt marks its credential suspect through SuspectFailed. The
// service runs the type's check_health handler at most once per cooldown
// window (default 5 minutes, per-type registration override) in a detached
// goroutine with a bounded timeout, coalescing concurrent suspects into one
// in-flight check. Only an explicit "unhealthy" verdict disables the
// account; "healthy", "unknown" and handler failures only stamp the last
// check time. Types without a check_health handler never check.
package healthcheck

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
)

// CheckTimeout bounds one detached health-check call.
const CheckTimeout = 30 * time.Second

// Record is one credential's health-check state, keyed by credential ID.
type Record struct {
	CredentialID string    `json:"credential_id"`
	LastCheckAt  time.Time `json:"last_check_at"`
	LastStatus   string    `json:"last_status,omitempty"`
}

// PluginHealth is the luaplugin surface this service needs.
type PluginHealth interface {
	HasHandler(typeKey, handler string) bool
	HealthCooldown(typeKey string) time.Duration
	CheckHealth(ctx context.Context, typeKey string, cred *models.Credential) (luaplugin.HealthStatus, string, error)
}

// CredentialStore fetches credentials and disables unhealthy accounts.
type CredentialStore interface {
	Get(id string) (*models.Credential, error)
	DisableUnhealthy(id, reason string) error
}

// Service dispatches failure-triggered health checks with per-credential
// cooldown and singleflight.
type Service struct {
	repo   *repository.Repository[Record]
	lua    PluginHealth
	creds  CredentialStore
	logger *slog.Logger

	mu       sync.Mutex
	inflight map[string]bool
}

// New constructs the healthcheck Service. lua and creds may be nil
// (checks disabled); SetLogger wires structured logging.
func New(database *db.DB, lua PluginHealth, creds CredentialStore) *Service {
	return &Service{
		repo:     repository.New[Record](database, db.BucketCredentialHealth, "healthcheck"),
		lua:      lua,
		creds:    creds,
		inflight: map[string]bool{},
	}
}

// SetLogger wires structured logging.
func (s *Service) SetLogger(l *slog.Logger) { s.logger = l }

// SuspectFailed records a failed attempt identity and starts a detached
// health check unless the type has no check_health handler, a check ran
// within the cooldown window, or one is already in flight. Never blocks;
// never fails the caller.
func (s *Service) SuspectFailed(pluginID, typeKey, credentialID string) {
	if s.lua == nil || s.creds == nil || credentialID == "" {
		return
	}
	if !s.lua.HasHandler(typeKey, string(luaplugin.HandlerCheckHealth)) {
		return
	}
	cooldown := s.lua.HealthCooldown(typeKey)
	if rec, err := s.repo.Get(credentialID); err == nil && rec != nil {
		if time.Now().Before(rec.LastCheckAt.Add(cooldown)) {
			return
		}
	}
	s.mu.Lock()
	if s.inflight[credentialID] {
		s.mu.Unlock()
		return
	}
	s.inflight[credentialID] = true
	s.mu.Unlock()
	go s.run(typeKey, credentialID)
}

func (s *Service) done(credentialID string) {
	s.mu.Lock()
	delete(s.inflight, credentialID)
	s.mu.Unlock()
}

// run executes one detached health check: fetch the credential, invoke the
// handler with a bounded timeout, stamp the outcome, and disable only on
// an explicit unhealthy verdict.
func (s *Service) run(typeKey, credentialID string) {
	defer s.done(credentialID)
	cred, err := s.creds.Get(credentialID)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), CheckTimeout)
	defer cancel()
	status, message, herr := s.lua.CheckHealth(ctx, typeKey, cred)
	if herr != nil {
		s.stamp(credentialID, string(luaplugin.HealthUnknown))
		if s.logger != nil {
			s.logger.Warn("healthcheck: handler failed, keeping account",
				"credential_id", credentialID, "type", typeKey, "error", herr)
		}
		return
	}
	s.stamp(credentialID, string(status))
	if s.logger != nil {
		s.logger.Debug("healthcheck: verdict recorded",
			"credential_id", credentialID, "type", typeKey, "status", string(status))
	}
	if status != luaplugin.HealthUnhealthy {
		return
	}
	reason := message
	if reason == "" {
		reason = "health check reported unhealthy"
	}
	if derr := s.creds.DisableUnhealthy(credentialID, reason); derr != nil && s.logger != nil {
		s.logger.Warn("healthcheck: disable failed", "credential_id", credentialID, "error", derr)
	} else if s.logger != nil {
		s.logger.Info("healthcheck: disabled unhealthy account", "credential_id", credentialID, "type", typeKey)
	}
}

func (s *Service) stamp(credentialID, status string) {
	_ = s.repo.Put(credentialID, &Record{
		CredentialID: credentialID,
		LastCheckAt:  time.Now(),
		LastStatus:   status,
	})
}
