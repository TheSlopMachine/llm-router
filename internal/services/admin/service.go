// Package admin manages the bootstrap flow and dashboard authentication.
package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
	"golang.org/x/crypto/bcrypt"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

const (
	bcryptCost                = 12
	sessionDurationNormal     = 8 * time.Hour
	sessionDurationRememberMe = 30 * 24 * time.Hour
)

// ErrInvalidCredentials is returned when a login attempt fails.
var ErrInvalidCredentials = errors.New("invalid username or password")

// Session is a persistent dashboard session.
type Session struct {
	Token      string    `json:"token"`
	Username   string    `json:"username"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	RememberMe bool      `json:"remember_me"`
}

// Service manages admin users and persistent dashboard sessions.
type Service struct {
	database        *db.DB
	providerService *provider.Service
	store           *repository.Repository[models.AdminUser]
}

// New constructs a new admin Service.
func New(database *db.DB, providerService *provider.Service) *Service {
	s := &Service{
		database:        database,
		providerService: providerService,
		store:           repository.New[models.AdminUser](database, db.BucketAdmin, "admin"),
	}
	s.CleanupExpiredSessions()
	return s
}

// ─────────────────────────────────────────────
// Bootstrap
// ─────────────────────────────────────────────

// DB returns the underlying database instance.
func (s *Service) DB() *db.DB {
	return s.database
}

// Bootstrap creates the initial admin account and marks the DB as bootstrapped.
// Returns an error if already bootstrapped.
func (s *Service) Bootstrap(username, password string) error {
	ok, err := s.database.IsBootstrapped()
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("already bootstrapped")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	user := &models.AdminUser{
		Username:     username,
		PasswordHash: string(hash),
		CreatedAt:    util.Now(),
	}

	if err := s.store.Put(username, user); err != nil {
		return err
	}

	return s.database.SetBootstrapped()
}

// ChangePassword verifies the current password and updates to a new one.
func (s *Service) ChangePassword(username, oldPassword, newPassword string) error {
	user, err := s.store.Get(username)
	if err != nil {
		return ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		return ErrInvalidCredentials
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	user.PasswordHash = string(hash)
	return s.store.Put(username, user)
}

// ─────────────────────────────────────────────
// Authentication
// ─────────────────────────────────────────────

// Login validates credentials and returns a session token with expiration.
func (s *Service) Login(username, password string, rememberMe bool) (string, time.Time, error) {
	user, err := s.store.Get(username)
	if err != nil {
		return "", time.Time{}, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
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
	expiresAt := util.Now().Add(duration)

	session := &Session{
		Token:      token,
		Username:   username,
		CreatedAt:  util.Now(),
		ExpiresAt:  expiresAt,
		RememberMe: rememberMe,
	}

	if err := s.database.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(db.BucketSessions)
		if err != nil {
			return err
		}
		data, err := json.Marshal(session)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(token), data)
	}); err != nil {
		return "", time.Time{}, err
	}

	return token, expiresAt, nil
}

// Logout invalidates a session token.
func (s *Service) Logout(token string) {
	s.deleteSession(token)
}

func (s *Service) deleteSession(token string) {
	s.database.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(db.BucketSessions)
		if bucket == nil {
			return nil
		}
		bucket.Delete([]byte(token))
		return nil
	})
}

// ValidateSession returns the username for a valid, non-expired session token.
// Auto-refreshes the session expiration on each call (sliding window).
func (s *Service) ValidateSession(token string) (string, bool) {
	var session Session

	if err := s.database.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(db.BucketSessions)
		if bucket == nil {
			return fmt.Errorf("bucket not found")
		}
		data := bucket.Get([]byte(token))
		if data == nil {
			return fmt.Errorf("session not found")
		}
		return json.Unmarshal(data, &session)
	}); err != nil {
		return "", false
	}

	if util.Now().After(session.ExpiresAt) {
		s.deleteSession(token)
		return "", false
	}

	duration := sessionDurationNormal
	if session.RememberMe {
		duration = sessionDurationRememberMe
	}
	session.ExpiresAt = util.Now().Add(duration)

	s.database.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(db.BucketSessions)
		if bucket == nil {
			return nil
		}
		data, _ := json.Marshal(session)
		bucket.Put([]byte(token), data)
		return nil
	})

	return session.Username, true
}

// CleanupExpiredSessions removes expired sessions from the database.
func (s *Service) CleanupExpiredSessions() error {
	threshold := util.Now()
	return repository.CleanupExpired(s.database, db.BucketSessions, threshold, func(session *Session) time.Time {
		return session.ExpiresAt
	})
}
