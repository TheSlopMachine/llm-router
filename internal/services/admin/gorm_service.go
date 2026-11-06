package admin

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	stdErrors "errors"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// passwordRowID pins the single dashboard_password row. The table CHECK
// enforces id = 1; the admin path never reads or writes another identifier.
const passwordRowID int64 = 1

// GormService manages the dashboard password and dashboard sessions on the
// SQLite handles. The dashboard has one operator and no accounts: the
// password hash lives in the durable database, sessions in the cache file.
// Session tokens persist as SHA-256 hashes only; the plaintext token returns
// once at login and never lands in storage. Every statement runs through
// gorm.G with typed clauses.
type GormService struct {
	database *db.Database
	logger   *slog.Logger
	// passwordSet caches a true answer: the password is never removed, so
	// once observed every later check skips the database.
	passwordSet atomic.Bool
}

// Session is the state of a validated dashboard session.
type Session struct {
	ExpiresAt  time.Time
	RememberMe bool
	// Refreshed reports that this validation moved ExpiresAt forward.
	Refreshed bool
}

// NewGorm constructs a GORM-backed admin service on the SQLite handle.
// Construction performs no I/O.
func NewGorm(database *db.Database) *GormService {
	return &GormService{database: database}
}

// SetLogger sets structured logging.
func (s *GormService) SetLogger(l *slog.Logger) { s.logger = l }

// requireHandle fails closed when either SQLite handle is missing.
func (s *GormService) requireHandle() (*db.Database, error) {
	if s == nil || s.database == nil || s.database.Handle == nil || s.database.Cache == nil {
		return nil, fmt.Errorf("admin sqlite handle is not configured: %w", apierrors.ErrInvalidInput)
	}
	return s.database, nil
}

// HasPassword reports whether the dashboard password is set. A true answer
// is cached: the password is never removed.
func (s *GormService) HasPassword(ctx context.Context) (bool, error) {
	if s != nil && s.passwordSet.Load() {
		return true, nil
	}
	if _, err := s.requireHandle(); err != nil {
		return false, err
	}
	_, found, err := s.loadPassword(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Check dashboard password failed", "error", err)
		}
		return false, err
	}
	if found {
		s.passwordSet.Store(true)
	}
	return found, nil
}

// SetPassword stores a new password hash without asking for the current
// password. Local database access is root-equivalent: this path serves CLI
// operators, never the dashboard. Existing sessions survive.
func (s *GormService) SetPassword(ctx context.Context, password string) error {
	if password == "" {
		return apierrors.NewValidationError("password", "must not be empty")
	}
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := s.storeHash(ctx, string(hash)); err != nil {
		if s.logger != nil {
			s.logger.Error("Set dashboard password failed", "error", err)
		}
		return err
	}
	s.passwordSet.Store(true)
	if s.logger != nil {
		s.logger.Info("Dashboard password set", "source", "cli")
	}
	return nil
}

// ChangePassword verifies the current password and stores the new hash. A
// wrong current password denies with ErrInvalidCredentials. Existing
// sessions survive.
func (s *GormService) ChangePassword(ctx context.Context, currentPassword, newPassword string) error {
	if newPassword == "" {
		return apierrors.NewValidationError("new_password", "must not be empty")
	}
	if err := s.verifyPassword(ctx, currentPassword); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := s.storeHash(ctx, string(hash)); err != nil {
		if s.logger != nil {
			s.logger.Error("Change dashboard password failed", "error", err)
		}
		return err
	}
	if s.logger != nil {
		s.logger.Info("Dashboard password changed")
	}
	return nil
}

// Login verifies the password and persists a hashed session. The plaintext
// token returns once and never persists. A wrong or unset password denies
// with ErrInvalidCredentials.
func (s *GormService) Login(ctx context.Context, password string, rememberMe bool) (string, time.Time, error) {
	if err := s.verifyPassword(ctx, password); err != nil {
		return "", time.Time{}, err
	}
	token, err := util.GenerateToken()
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Login failed", "error", err)
		}
		return "", time.Time{}, err
	}
	now := util.Now()
	expiresAt := now.Add(sessionDuration(rememberMe))
	session := sessionRecordFromLogin(util.HashSecret(token), now, expiresAt, rememberMe)
	if err := s.database.Cache.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		return db.Translate(gorm.G[record.Session](transaction).Create(ctx, &session))
	}); err != nil {
		if s.logger != nil {
			s.logger.Error("Login failed", "error", err)
		}
		return "", time.Time{}, err
	}
	if s.logger != nil {
		s.logger.Debug("Dashboard login succeeded", "remember_me", rememberMe)
	}
	return token, expiresAt, nil
}

// Logout invalidates a session token. Missing tokens are not errors.
func (s *GormService) Logout(ctx context.Context, token string) {
	if _, err := s.requireHandle(); err != nil {
		return
	}
	_ = s.database.Cache.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		_, err := gorm.G[record.Session](transaction, clause.Eq{Column: "token_hash", Value: util.HashSecret(token)}).Delete(ctx)
		return db.Translate(err)
	})
	if s.logger != nil {
		s.logger.Debug("Session logout completed")
	}
}

// ValidateSession reports whether a session token is valid and not expired.
// The common path is read-only: the expiration moves forward only when less
// than half the session duration remains, so parallel dashboard calls never
// contend on one row. A failed refresh never invalidates a verified session.
// Expired sessions delete best-effort.
func (s *GormService) ValidateSession(ctx context.Context, token string) (Session, bool) {
	if _, err := s.requireHandle(); err != nil {
		return Session{}, false
	}
	tokenHash := util.HashSecret(token)
	row, err := gorm.G[record.Session](s.database.Cache, clause.Eq{Column: "token_hash", Value: tokenHash}).First(ctx)
	if err != nil {
		return Session{}, false
	}
	now := util.Now()
	if now.After(row.ExpiresAt) {
		_ = s.database.Cache.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			_, err := gorm.G[record.Session](transaction, clause.Eq{Column: "token_hash", Value: tokenHash}).Delete(ctx)
			return db.Translate(err)
		})
		if s.logger != nil {
			s.logger.Debug("Session expired")
		}
		return Session{}, false
	}
	session := Session{ExpiresAt: row.ExpiresAt, RememberMe: row.RememberMe}
	duration := sessionDuration(row.RememberMe)
	if now.Add(duration / 2).After(row.ExpiresAt) {
		expiresAt := now.Add(duration)
		err := s.database.Cache.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			_, err := gorm.G[record.Session](transaction, clause.Eq{Column: "token_hash", Value: tokenHash}).Update(ctx, "expires_at", expiresAt.UTC())
			return db.Translate(err)
		})
		if err == nil {
			session.ExpiresAt, session.Refreshed = expiresAt, true
			if s.logger != nil {
				s.logger.Debug("Session refreshed")
			}
		}
	}
	return session, true
}

// CleanupExpiredSessions removes sessions expired before now through the
// expires_at index on the cache handle. Valid sessions survive.
func (s *GormService) CleanupExpiredSessions(ctx context.Context) error {
	if _, err := s.requireHandle(); err != nil {
		if s.logger != nil {
			s.logger.Error("Cleanup expired sessions failed", "error", err)
		}
		return err
	}
	var removed int64
	err := s.database.Cache.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		tracker := gorm.WithResult()
		if _, err := gorm.G[record.Session](transaction, clause.Lt{Column: "expires_at", Value: util.Now()}, tracker).Delete(ctx); err != nil {
			return db.Translate(err)
		}
		removed = tracker.RowsAffected
		return nil
	})
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Cleanup expired sessions failed", "error", err)
		}
		return err
	}
	if s.logger != nil {
		s.logger.Info("Expired sessions removed", "count", removed)
	}
	return nil
}

// verifyPassword compares a candidate against the stored hash. An unset
// password costs the same bcrypt time as a wrong one and denies the same way.
func (s *GormService) verifyPassword(ctx context.Context, password string) error {
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	hash, found, err := s.loadPassword(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Password check failed", "error", err)
		}
		return err
	}
	if !found {
		_ = bcrypt.CompareHashAndPassword(timingEqualizerHash, []byte(password))
		if s.logger != nil {
			s.logger.Warn("Invalid password", "reason", "password_not_set")
		}
		return ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		if s.logger != nil {
			s.logger.Warn("Invalid password")
		}
		return ErrInvalidCredentials
	}
	return nil
}

// loadPassword reads the stored hash. A missing row reports found = false.
func (s *GormService) loadPassword(ctx context.Context) (string, bool, error) {
	row, err := gorm.G[record.DashboardPassword](s.database.Handle, clause.Eq{Column: "id", Value: passwordRowID}).First(ctx)
	if err != nil {
		translated := db.Translate(err)
		if stdErrors.Is(translated, apierrors.ErrNotFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("get dashboard password: %w", translated)
	}
	return row.PasswordHash, row.PasswordHash != "", nil
}

// storeHash upserts the single password row in one transaction.
func (s *GormService) storeHash(ctx context.Context, hash string) error {
	row := record.DashboardPassword{ID: passwordRowID, PasswordHash: hash, UpdatedAt: util.Now().UTC()}
	return s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if err := gorm.G[record.DashboardPassword](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(ctx, &row); err != nil {
			return fmt.Errorf("store dashboard password: %w", db.Translate(err))
		}
		return nil
	})
}
