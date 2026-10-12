package admin

import (
	"context"
	"sync"
	"testing"
	"time"

	stdErrors "errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

func setupGormAdminService(t *testing.T) (*GormService, *db.Database, context.Context) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	return NewGorm(database), database, context.Background()
}

func TestGormService_Bootstrap_SetsFlagAndUser(t *testing.T) {
	service, database, ctx := setupGormAdminService(t)

	if err := service.Bootstrap(ctx, "operator", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}

	user, err := gorm.G[record.AdminUser](database.Handle, clause.Eq{Column: "username", Value: "operator"}).First(ctx)
	if err != nil {
		t.Fatalf("get admin user failed: %v", err)
	}
	if user.PasswordHash == "" {
		t.Error("password hash should not be empty")
	}
	if user.PasswordHash == "password123" {
		t.Error("password must persist hashed, never plaintext")
	}

	settings, err := gorm.G[record.InstanceSetting](database.Handle, clause.Eq{Column: "id", Value: instanceRowID}).First(ctx)
	if err != nil {
		t.Fatalf("get instance settings failed: %v", err)
	}
	if !settings.Bootstrapped {
		t.Error("bootstrap should set the instance bootstrapped flag")
	}
}

func TestGormService_Bootstrap_AlreadyBootstrapped(t *testing.T) {
	service, _, ctx := setupGormAdminService(t)

	if err := service.Bootstrap(ctx, "operator", "password123"); err != nil {
		t.Fatalf("first bootstrap failed: %v", err)
	}
	if err := service.Bootstrap(ctx, "operator2", "password456"); err == nil {
		t.Error("expected error for second bootstrap, got nil")
	}
}

func TestGormService_Bootstrap_MissingHandleFailsClosed(t *testing.T) {
	service := NewGorm(nil)
	if err := service.Bootstrap(context.Background(), "operator", "password123"); !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Errorf("bootstrap without handle: want invalid-input, got %v", err)
	}
}

func TestGormService_Login_ValidCredentials(t *testing.T) {
	service, _, ctx := setupGormAdminService(t)

	if err := service.Bootstrap(ctx, "operator", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	token, expiresAt, err := service.Login(ctx, "operator", "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if token == "" {
		t.Error("token should not be empty")
	}
	if expiresAt.IsZero() {
		t.Error("expires at should not be zero")
	}
	if time.Until(expiresAt) <= 0 {
		t.Error("expires at should be in the future")
	}
}

func TestGormService_Login_StoresHashOnly(t *testing.T) {
	service, database, ctx := setupGormAdminService(t)

	if err := service.Bootstrap(ctx, "operator", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	token, _, err := service.Login(ctx, "operator", "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	rows, err := gorm.G[record.Session](database.Cache).Find(ctx)
	if err != nil {
		t.Fatalf("list sessions failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("sessions length: got %d, want 1", len(rows))
	}
	if rows[0].TokenHash != util.HashSecret(token) {
		t.Error("session must persist under the SHA-256 token hash")
	}
	if rows[0].TokenHash == token {
		t.Error("plaintext token must never land in storage")
	}
	if rows[0].Username != "operator" {
		t.Errorf("username: got %q, want %q", rows[0].Username, "operator")
	}
}

func TestGormService_Login_InvalidCredentials(t *testing.T) {
	service, _, ctx := setupGormAdminService(t)

	if err := service.Bootstrap(ctx, "operator", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	if _, _, err := service.Login(ctx, "operator", "wrongpassword", false); err != ErrInvalidCredentials {
		t.Errorf("wrong password: want ErrInvalidCredentials, got %v", err)
	}
	if _, _, err := service.Login(ctx, "missing", "password123", false); err != ErrInvalidCredentials {
		t.Errorf("missing user: want ErrInvalidCredentials, got %v", err)
	}
}

func TestGormService_Login_RememberMe(t *testing.T) {
	service, _, ctx := setupGormAdminService(t)

	if err := service.Bootstrap(ctx, "operator", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	token, expiresAt, err := service.Login(ctx, "operator", "password123", true)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if token == "" {
		t.Error("token should not be empty")
	}
	if duration := time.Until(expiresAt); duration < 24*time.Hour {
		t.Errorf("remember me duration too short: %v", duration)
	}
}

func TestGormService_ValidateSession_Valid(t *testing.T) {
	service, _, ctx := setupGormAdminService(t)

	if err := service.Bootstrap(ctx, "operator", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	token, _, err := service.Login(ctx, "operator", "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	username, valid := service.ValidateSession(ctx, token)
	if !valid {
		t.Error("session should be valid")
	}
	if username != "operator" {
		t.Errorf("username: got %q, want %q", username, "operator")
	}
}

func TestGormService_ValidateSession_Invalid(t *testing.T) {
	service, _, ctx := setupGormAdminService(t)

	if _, valid := service.ValidateSession(ctx, "invalid-token"); valid {
		t.Error("session should not be valid")
	}
}

func TestGormService_ValidateSession_SlidingWindow(t *testing.T) {
	service, database, ctx := setupGormAdminService(t)

	if err := service.Bootstrap(ctx, "operator", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	token, _, err := service.Login(ctx, "operator", "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	// Move the expiration near so validation refreshes it. Fresh sessions
	// hold nearly the full duration and validate read-only.
	near := util.Now().Add(time.Hour)
	if _, err := gorm.G[record.Session](database.Cache, clause.Eq{Column: "token_hash", Value: util.HashSecret(token)}).Update(ctx, "expires_at", near); err != nil {
		t.Fatalf("move expiration near failed: %v", err)
	}

	before, err := gorm.G[record.Session](database.Cache, clause.Eq{Column: "token_hash", Value: util.HashSecret(token)}).First(ctx)
	if err != nil {
		t.Fatalf("get session failed: %v", err)
	}

	username, valid := service.ValidateSession(ctx, token)
	if !valid {
		t.Fatal("session should be valid")
	}
	if username != "operator" {
		t.Errorf("username: got %q, want %q", username, "operator")
	}

	after, err := gorm.G[record.Session](database.Cache, clause.Eq{Column: "token_hash", Value: util.HashSecret(token)}).First(ctx)
	if err != nil {
		t.Fatalf("get refreshed session failed: %v", err)
	}
	if !after.ExpiresAt.After(before.ExpiresAt) {
		t.Error("validate should extend the session expiration")
	}
}

func TestGormService_ValidateSession_Concurrent(t *testing.T) {
	service, _, ctx := setupGormAdminService(t)

	if err := service.Bootstrap(ctx, "operator", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	token, _, err := service.Login(ctx, "operator", "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	const callers = 16
	var group sync.WaitGroup
	rejected := make(chan string, callers)
	for i := 0; i < callers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			username, valid := service.ValidateSession(ctx, token)
			if !valid || username != "operator" {
				rejected <- username
			}
		}()
	}
	group.Wait()
	close(rejected)
	for username := range rejected {
		t.Errorf("concurrent validate rejected session, username %q", username)
	}
}

func TestGormService_ValidateSession_ExpiredDeletesRow(t *testing.T) {
	service, database, ctx := setupGormAdminService(t)

	if err := service.Bootstrap(ctx, "operator", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	token, _, err := service.Login(ctx, "operator", "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	past := util.Now().Add(-time.Hour)
	tracker := gorm.WithResult()
	if _, err := gorm.G[record.Session](database.Cache, clause.Eq{Column: "token_hash", Value: util.HashSecret(token)}, tracker).Update(ctx, "expires_at", past); err != nil {
		t.Fatalf("expire session failed: %v", err)
	}

	if _, valid := service.ValidateSession(ctx, token); valid {
		t.Fatal("expired session should not be valid")
	}
	count, err := gorm.G[record.Session](database.Cache, clause.Eq{Column: "token_hash", Value: util.HashSecret(token)}).Count(ctx, "*")
	if err != nil {
		t.Fatalf("count sessions failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expired session should delete on validate, holds %d rows", count)
	}
}

func TestGormService_Logout(t *testing.T) {
	service, _, ctx := setupGormAdminService(t)

	if err := service.Bootstrap(ctx, "operator", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	token, _, err := service.Login(ctx, "operator", "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if _, valid := service.ValidateSession(ctx, token); !valid {
		t.Fatal("session should be valid before logout")
	}
	service.Logout(ctx, token)
	if _, valid := service.ValidateSession(ctx, token); valid {
		t.Error("session should be invalid after logout")
	}
	service.Logout(ctx, token)
}

func TestGormService_CleanupExpiredSessions(t *testing.T) {
	service, database, ctx := setupGormAdminService(t)

	if err := service.Bootstrap(ctx, "operator", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	kept, _, err := service.Login(ctx, "operator", "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	expired, _, err := service.Login(ctx, "operator", "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	past := util.Now().Add(-time.Hour)
	if _, err := gorm.G[record.Session](database.Cache, clause.Eq{Column: "token_hash", Value: util.HashSecret(expired)}).Update(ctx, "expires_at", past); err != nil {
		t.Fatalf("expire session failed: %v", err)
	}

	if err := service.CleanupExpiredSessions(ctx); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	if _, valid := service.ValidateSession(ctx, kept); !valid {
		t.Error("valid session should survive cleanup")
	}
	count, err := gorm.G[record.Session](database.Cache, clause.Eq{Column: "token_hash", Value: util.HashSecret(expired)}).Count(ctx, "*")
	if err != nil {
		t.Fatalf("count sessions failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expired session should remove on cleanup, holds %d rows", count)
	}
}

func TestGormService_ChangePassword(t *testing.T) {
	service, _, ctx := setupGormAdminService(t)

	if err := service.Bootstrap(ctx, "operator", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	if err := service.ChangePassword(ctx, "operator", "password123", "password456"); err != nil {
		t.Fatalf("change password failed: %v", err)
	}
	if _, _, err := service.Login(ctx, "operator", "password123", false); err != ErrInvalidCredentials {
		t.Errorf("old password: want ErrInvalidCredentials, got %v", err)
	}
	if _, _, err := service.Login(ctx, "operator", "password456", false); err != nil {
		t.Errorf("new password login failed: %v", err)
	}
}

func TestGormService_ChangePassword_InvalidCredentials(t *testing.T) {
	service, _, ctx := setupGormAdminService(t)

	if err := service.Bootstrap(ctx, "operator", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	if err := service.ChangePassword(ctx, "operator", "wrongpassword", "password456"); err != ErrInvalidCredentials {
		t.Errorf("wrong old password: want ErrInvalidCredentials, got %v", err)
	}
	if err := service.ChangePassword(ctx, "missing", "password123", "password456"); err != ErrInvalidCredentials {
		t.Errorf("missing user: want ErrInvalidCredentials, got %v", err)
	}
}

func TestAdminUserConvert_RoundTrip(t *testing.T) {
	now := util.Now()
	user := adminUserModelFromRecord(adminUserRecordFromModel(adminUserModelFromRecord(record.AdminUser{Username: "operator", PasswordHash: "hash", CreatedAt: now})))
	if user.Username != "operator" || user.PasswordHash != "hash" {
		t.Errorf("converter roundtrip lost fields: %+v", user)
	}
	if !user.CreatedAt.Equal(now.UTC()) {
		t.Errorf("converter roundtrip time: got %v, want %v", user.CreatedAt, now.UTC())
	}
}
