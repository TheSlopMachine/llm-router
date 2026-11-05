package admin

import (
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

const (
	bcryptCost                = 12
	sessionDurationNormal     = 8 * time.Hour
	sessionDurationRememberMe = 30 * 24 * time.Hour
)

// ErrInvalidCredentials is returned when a login attempt fails.
var ErrInvalidCredentials = errors.New("invalid username or password")

// ErrAlreadyBootstrapped is returned when bootstrap runs on an instance that
// already has its first admin account.
var ErrAlreadyBootstrapped = errors.New("already bootstrapped")

// timingEqualizerHash is a bcrypt hash compared against when the username
// does not exist, so a missing account costs the same time as a wrong
// password and login latency never reveals which accounts exist.
var timingEqualizerHash, _ = bcrypt.GenerateFromPassword([]byte("llm-router timing equalizer"), bcryptCost)

// adminUserRecordFromModel maps the wire account onto the admin_users row.
// The username is the primary key; timestamps normalize to UTC.
func adminUserRecordFromModel(user *models.AdminUser) record.AdminUser {
	return record.AdminUser{
		Username:     user.Username,
		PasswordHash: user.PasswordHash,
		CreatedAt:    user.CreatedAt.UTC(),
	}
}

// adminUserModelFromRecord rebuilds the wire account from the admin_users
// row. Timestamps return in UTC.
func adminUserModelFromRecord(row record.AdminUser) *models.AdminUser {
	return &models.AdminUser{
		Username:     row.Username,
		PasswordHash: row.PasswordHash,
		CreatedAt:    row.CreatedAt.UTC(),
	}
}

// sessionRecordFromLogin builds the sessions row for a fresh login. The
// token hash is the primary key so the plaintext token never lands in
// storage. Timestamps normalize to UTC.
func sessionRecordFromLogin(username, tokenHash string, createdAt, expiresAt time.Time, rememberMe bool) record.Session {
	return record.Session{
		TokenHash:  tokenHash,
		Username:   username,
		CreatedAt:  createdAt.UTC(),
		ExpiresAt:  expiresAt.UTC(),
		RememberMe: rememberMe,
	}
}
