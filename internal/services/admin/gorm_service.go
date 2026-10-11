package admin

import (
	"context"
	"fmt"
	"time"

	stdErrors "errors"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// instanceRowID pins the single instance_settings row. The table CHECK
// enforces id = 1; the admin path never reads or writes another identifier.
const instanceRowID int64 = 1

// GormService manages admin users and dashboard sessions on the SQLite
// handle with explicit converters between wire types and rows. Session
// tokens persist as SHA-256 hashes only; the plaintext token returns once
// at login and never lands in storage. Traditional GORM use: none; every
// statement runs through gorm.G with typed clauses.
type GormService struct {
	database *db.Database
}

// NewGorm constructs a GORM-backed admin service on the SQLite handle.
// Construction performs no I/O.
func NewGorm(database *db.Database) *GormService {
	return &GormService{database: database}
}

// requireHandle fails closed when the SQLite handle is missing.
func (s *GormService) requireHandle() (*db.Database, error) {
	if s == nil || s.database == nil || s.database.Handle == nil {
		return nil, fmt.Errorf("admin sqlite handle is not configured: %w", apierrors.ErrInvalidInput)
	}
	return s.database, nil
}

// Bootstrap creates the initial admin account and marks the instance
// bootstrapped. A set flag denies with an already-bootstrapped error. The
// password hash generates outside the transaction; one transaction holds
// the flag check, the user upsert, and the flag write on the transaction
// handle only.
func (s *GormService) Bootstrap(ctx context.Context, username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	user := &models.AdminUser{
		Username:     username,
		PasswordHash: string(hash),
		CreatedAt:    util.Now(),
	}
	return s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		settings, err := loadInstanceSettings(ctx, transaction)
		if err != nil {
			return err
		}
		if settings.Bootstrapped {
			return fmt.Errorf("already bootstrapped")
		}
		row := adminUserRecordFromModel(user)
		rows := []record.AdminUser{row}
		if err := gorm.G[record.AdminUser](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "username"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
			return db.Translate(err)
		}
		settings.Bootstrapped = true
		settings.UpdatedAt = util.Now()
		return upsertInstanceSettings(ctx, transaction, settings)
	})
}

// IsBootstrapped returns true once the admin account has been created.
// A missing settings row reads as not bootstrapped.
func (s *GormService) IsBootstrapped(ctx context.Context) (bool, error) {
	if _, err := s.requireHandle(); err != nil {
		return false, err
	}
	var bootstrapped bool
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		settings, err := loadInstanceSettings(ctx, transaction)
		if err != nil {
			return err
		}
		bootstrapped = settings.Bootstrapped
		return nil
	}); err != nil {
		return false, err
	}
	return bootstrapped, nil
}

// ChangePassword verifies the current password and stores the new hash. A
// missing account or a wrong password denies with ErrInvalidCredentials.
// Verification and hashing run outside the transaction; one transaction
// holds the single-column update on the transaction handle only.
func (s *GormService) ChangePassword(ctx context.Context, username, oldPassword, newPassword string) error {
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	row, err := gorm.G[record.AdminUser](s.database.Handle, clause.Eq{Column: "username", Value: username}).First(ctx)
	if err != nil {
		return ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte(oldPassword)); err != nil {
		return ErrInvalidCredentials
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	return s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		tracker := gorm.WithResult()
		affected, err := gorm.G[record.AdminUser](transaction, clause.Eq{Column: "username", Value: username}, tracker).Update(ctx, "password_hash", string(hash))
		if err != nil {
			return db.Translate(err)
		}
		if affected == 0 || tracker.RowsAffected == 0 {
			return ErrInvalidCredentials
		}
		return nil
	})
}

// Login validates credentials and persists a hashed session with
// expiration. The plaintext token returns once and never persists. Unknown
// accounts and wrong passwords deny with ErrInvalidCredentials. The
// credential check runs outside the transaction; one transaction holds the
// session insert on the transaction handle only.
func (s *GormService) Login(ctx context.Context, username, password string, rememberMe bool) (string, time.Time, error) {
	if _, err := s.requireHandle(); err != nil {
		return "", time.Time{}, err
	}
	row, err := gorm.G[record.AdminUser](s.database.Handle, clause.Eq{Column: "username", Value: username}).First(ctx)
	if err != nil {
		return "", time.Time{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte(password)); err != nil {
		return "", time.Time{}, ErrInvalidCredentials
	}
	token, err := util.GenerateToken()
	if err != nil {
		return "", time.Time{}, err
	}
	duration := sessionDurationNormal
	if rememberMe {
		duration = sessionDurationRememberMe
	}
	now := util.Now()
	expiresAt := now.Add(duration)
	session := sessionRecordFromLogin(username, util.HashSecret(token), now, expiresAt, rememberMe)
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if err := gorm.G[record.Session](transaction).Create(ctx, &session); err != nil {
			translated := db.Translate(err)
			if stdErrors.Is(translated, apierrors.ErrInvalidInput) {
				return ErrInvalidCredentials
			}
			return translated
		}
		return nil
	}); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// Logout invalidates a session token. Missing tokens are not errors,
// matching the previous behavior.
func (s *GormService) Logout(ctx context.Context, token string) {
	if _, err := s.requireHandle(); err != nil {
		return
	}
	database := s.database
	_ = database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		_, err := gorm.G[record.Session](transaction, clause.Eq{Column: "token_hash", Value: util.HashSecret(token)}).Delete(ctx)
		return db.Translate(err)
	})
}

// ValidateSession returns the username for a valid, non-expired session
// token. The common path is read-only: the expiration refreshes only when
// less than half the session duration remains, so parallel dashboard calls
// never contend on one row. A failed refresh never invalidates a verified
// session. Expired sessions delete best-effort. Unknown or expired tokens
// deny with false, never an error, matching the previous behavior.
func (s *GormService) ValidateSession(ctx context.Context, token string) (string, bool) {
	if _, err := s.requireHandle(); err != nil {
		return "", false
	}
	tokenHash := util.HashSecret(token)
	session, err := gorm.G[record.Session](s.database.Handle, clause.Eq{Column: "token_hash", Value: tokenHash}).First(ctx)
	if err != nil {
		return "", false
	}
	now := util.Now()
	if now.After(session.ExpiresAt) {
		_ = s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			_, err := gorm.G[record.Session](transaction, clause.Eq{Column: "token_hash", Value: tokenHash}).Delete(ctx)
			return db.Translate(err)
		})
		return "", false
	}
	duration := sessionDurationNormal
	if session.RememberMe {
		duration = sessionDurationRememberMe
	}
	if now.Add(duration / 2).After(session.ExpiresAt) {
		_ = s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			_, err := gorm.G[record.Session](transaction, clause.Eq{Column: "token_hash", Value: tokenHash}).Update(ctx, "expires_at", now.Add(duration))
			return db.Translate(err)
		})
	}
	return session.Username, true
}

// CleanupExpiredSessions removes sessions expired before now through the
// expires_at index. Valid sessions survive.
func (s *GormService) CleanupExpiredSessions(ctx context.Context) error {
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	return s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		_, err := gorm.G[record.Session](transaction, clause.Lt{Column: "expires_at", Value: util.Now()}).Delete(ctx)
		return db.Translate(err)
	})
}

// loadInstanceSettings reads row 1 through the transaction handle. A miss
// returns the default row, never a not-found error: writers merge into it.
func loadInstanceSettings(ctx context.Context, transaction *gorm.DB) (record.InstanceSetting, error) {
	row, err := gorm.G[record.InstanceSetting](transaction, clause.Eq{Column: "id", Value: instanceRowID}).First(ctx)
	if err != nil {
		if stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
			return record.InstanceSetting{ID: instanceRowID, ModelsFilter: "all"}, nil
		}
		return record.InstanceSetting{}, fmt.Errorf("get instance settings: %w", db.Translate(err))
	}
	return row, nil
}

// upsertInstanceSettings writes one row through the transaction handle. The
// conflict target is the primary key column only, so constraint violations
// surface as translated errors instead of overwrites.
func upsertInstanceSettings(ctx context.Context, transaction *gorm.DB, row record.InstanceSetting) error {
	if err := gorm.G[record.InstanceSetting](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(ctx, &row); err != nil {
		return fmt.Errorf("upsert instance settings: %w", db.Translate(err))
	}
	return nil
}
