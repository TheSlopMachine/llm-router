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
	db          *db.DB
	providerSvc *provider.Service
	repo        *repository.Repository[models.AdminUser]
}

// New constructs a new admin Service.
func New(database *db.DB, providerSvc *provider.Service) *Service {
	s := &Service{
		db:          database,
		providerSvc: providerSvc,
		repo:        repository.New[models.AdminUser](database, db.BucketAdmin, "admin"),
	}
	s.CleanupExpiredSessions()
	return s
}

// ─────────────────────────────────────────────
// Bootstrap
// ─────────────────────────────────────────────

// Bootstrap creates the initial admin account and marks the DB as bootstrapped.
// Returns an error if already bootstrapped.
func (s *Service) Bootstrap(username, password string) error {
	ok, err := s.db.IsBootstrapped()
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

	if err := s.repo.Put(username, user); err != nil {
		return err
	}

	if err := s.providerSvc.SyncDefaultProviders(); err != nil {
		return fmt.Errorf("sync default providers: %w", err)
	}

	return s.db.SetBootstrapped()
}

// ─────────────────────────────────────────────
// Authentication
// ─────────────────────────────────────────────

// Login validates credentials and returns a session token with expiration.
func (s *Service) Login(username, password string, rememberMe bool) (string, time.Time, error) {
	user, err := s.repo.Get(username)
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

	sess := &Session{
		Token:      token,
		Username:   username,
		CreatedAt:  util.Now(),
		ExpiresAt:  expiresAt,
		RememberMe: rememberMe,
	}

	if err := s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(db.BucketSessions)
		if err != nil {
			return err
		}
		data, err := json.Marshal(sess)
		if err != nil {
			return err
		}
		return b.Put([]byte(token), data)
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
	s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketSessions)
		if b == nil {
			return nil
		}
		b.Delete([]byte(token))
		return nil
	})
}

// ValidateSession returns the username for a valid, non-expired session token.
// Auto-refreshes the session expiration on each call (sliding window).
func (s *Service) ValidateSession(token string) (string, bool) {
	var sess Session

	if err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketSessions)
		if b == nil {
			return fmt.Errorf("bucket not found")
		}
		data := b.Get([]byte(token))
		if data == nil {
			return fmt.Errorf("session not found")
		}
		return json.Unmarshal(data, &sess)
	}); err != nil {
		return "", false
	}

	if util.Now().After(sess.ExpiresAt) {
		s.deleteSession(token)
		return "", false
	}

	duration := sessionDurationNormal
	if sess.RememberMe {
		duration = sessionDurationRememberMe
	}
	sess.ExpiresAt = util.Now().Add(duration)

	s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketSessions)
		if b == nil {
			return nil
		}
		data, _ := json.Marshal(sess)
		b.Put([]byte(token), data)
		return nil
	})

	return sess.Username, true
}

// CleanupExpiredSessions removes expired sessions from the database.
func (s *Service) CleanupExpiredSessions() error {
	threshold := util.Now()
	return repository.CleanupExpired(s.db, db.BucketSessions, threshold, func(sess *Session) time.Time {
		return sess.ExpiresAt
	})
}

