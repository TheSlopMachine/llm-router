package admin

import (
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
)

const (
	bcryptCost                = 12
	sessionDurationNormal     = 8 * time.Hour
	sessionDurationRememberMe = 30 * 24 * time.Hour
)

// ErrInvalidCredentials is returned when a password check fails.
var ErrInvalidCredentials = errors.New("invalid password")

// ErrPasswordNotSet is returned when the server starts without a dashboard
// password.
var ErrPasswordNotSet = errors.New("dashboard password is not set")

// timingEqualizerHash is a bcrypt hash compared against while no password is
// set, so an unset password costs the same time as a wrong one.
var timingEqualizerHash, _ = bcrypt.GenerateFromPassword([]byte("llm-router timing equalizer"), bcryptCost)

// sessionDuration is the idle lifetime of a session: ValidateSession moves
// the expiration forward while the session is in use.
func sessionDuration(rememberMe bool) time.Duration {
	if rememberMe {
		return sessionDurationRememberMe
	}
	return sessionDurationNormal
}

// sessionRecordFromLogin builds the sessions row for a fresh login. The
// token hash is the primary key so the plaintext token never lands in
// storage. Timestamps normalize to UTC.
func sessionRecordFromLogin(tokenHash string, createdAt, expiresAt time.Time, rememberMe bool) record.Session {
	return record.Session{
		TokenHash:  tokenHash,
		CreatedAt:  createdAt.UTC(),
		ExpiresAt:  expiresAt.UTC(),
		RememberMe: rememberMe,
	}
}
