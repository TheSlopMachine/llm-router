package admin

import (
	"errors"
	"time"

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
