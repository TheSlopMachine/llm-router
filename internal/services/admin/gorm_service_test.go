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

// setupWithPassword returns a service whose password is "password123".
func setupWithPassword(t *testing.T) (*GormService, *db.Database, context.Context) {
	t.Helper()
	service, database, ctx := setupGormAdminService(t)
	if err := service.SetPassword(ctx, "password123"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	return service, database, ctx
}

func TestGormService_HasPassword(t *testing.T) {
	service, _, ctx := setupGormAdminService(t)

	set, err := service.HasPassword(ctx)
	if err != nil {
		t.Fatalf("has password: %v", err)
	}
	if set {
		t.Fatal("fresh database must report no password")
	}
	if err := service.SetPassword(ctx, "password123"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	set, err = service.HasPassword(ctx)
	if err != nil {
		t.Fatalf("has password: %v", err)
	}
	if !set {
		t.Fatal("password must report set after SetPassword")
	}
}

func TestGormService_SetPassword_StoresHashOnly(t *testing.T) {
	_, database, ctx := setupWithPassword(t)

	row, err := gorm.G[record.DashboardPassword](database.Handle, clause.Eq{Column: "id", Value: passwordRowID}).First(ctx)
	if err != nil {
		t.Fatalf("get password row failed: %v", err)
	}
	if row.PasswordHash == "" || row.PasswordHash == "password123" {
		t.Errorf("password must persist hashed, got %q", row.PasswordHash)
	}
}

func TestGormService_SetPassword_ReplacesAndKeepsSessions(t *testing.T) {
	service, _, ctx := setupWithPassword(t)

	token, _, err := service.Login(ctx, "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if err := service.SetPassword(ctx, "password456"); err != nil {
		t.Fatalf("replace password failed: %v", err)
	}
	if _, _, err := service.Login(ctx, "password123", false); err != ErrInvalidCredentials {
		t.Errorf("old password: want ErrInvalidCredentials, got %v", err)
	}
	if _, _, err := service.Login(ctx, "password456", false); err != nil {
		t.Errorf("new password login failed: %v", err)
	}
	if _, valid := service.ValidateSession(ctx, token); !valid {
		t.Error("existing sessions must survive a password reset")
	}
}

func TestGormService_SetPassword_Validation(t *testing.T) {
	service, _, ctx := setupGormAdminService(t)
	if err := service.SetPassword(ctx, ""); !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Errorf("empty password: want invalid-input, got %v", err)
	}
}

func TestGormService_MissingHandleFailsClosed(t *testing.T) {
	service := NewGorm(nil)
	ctx := context.Background()
	if _, err := service.HasPassword(ctx); !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Errorf("has password without handle: want invalid-input, got %v", err)
	}
	if err := service.SetPassword(ctx, "password123"); !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Errorf("set password without handle: want invalid-input, got %v", err)
	}
}

func TestGormService_Login_ValidPassword(t *testing.T) {
	service, _, ctx := setupWithPassword(t)

	token, expiresAt, err := service.Login(ctx, "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if token == "" {
		t.Error("token should not be empty")
	}
	if time.Until(expiresAt) <= 0 {
		t.Error("expires at should be in the future")
	}
}

func TestGormService_Login_StoresHashOnly(t *testing.T) {
	service, database, ctx := setupWithPassword(t)

	token, _, err := service.Login(ctx, "password123", true)
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
	if !rows[0].RememberMe {
		t.Error("remember me must persist on the session")
	}
}

func TestGormService_Login_InvalidPassword(t *testing.T) {
	service, _, ctx := setupWithPassword(t)
	if _, _, err := service.Login(ctx, "wrongpassword", false); err != ErrInvalidCredentials {
		t.Errorf("wrong password: want ErrInvalidCredentials, got %v", err)
	}
}

func TestGormService_Login_NoPasswordDenies(t *testing.T) {
	service, _, ctx := setupGormAdminService(t)
	if _, _, err := service.Login(ctx, "", false); err != ErrInvalidCredentials {
		t.Errorf("unset password: want ErrInvalidCredentials, got %v", err)
	}
}

func TestGormService_Login_RememberMeDuration(t *testing.T) {
	service, _, ctx := setupWithPassword(t)

	_, normal, err := service.Login(ctx, "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	_, remembered, err := service.Login(ctx, "password123", true)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if duration := time.Until(normal); duration > sessionDurationNormal {
		t.Errorf("normal session duration too long: %v", duration)
	}
	if duration := time.Until(remembered); duration < 24*time.Hour {
		t.Errorf("remember me duration too short: %v", duration)
	}
}

func TestGormService_ValidateSession_Valid(t *testing.T) {
	service, _, ctx := setupWithPassword(t)

	token, expiresAt, err := service.Login(ctx, "password123", true)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	session, valid := service.ValidateSession(ctx, token)
	if !valid {
		t.Fatal("session should be valid")
	}
	if !session.RememberMe || session.Refreshed {
		t.Errorf("fresh remembered session: got %+v", session)
	}
	if !session.ExpiresAt.Equal(expiresAt.UTC()) {
		t.Errorf("expires at: got %v, want %v", session.ExpiresAt, expiresAt.UTC())
	}
}

func TestGormService_ValidateSession_Invalid(t *testing.T) {
	service, _, ctx := setupGormAdminService(t)
	if _, valid := service.ValidateSession(ctx, "invalid-token"); valid {
		t.Error("session should not be valid")
	}
}

func TestGormService_ValidateSession_SlidingWindow(t *testing.T) {
	service, database, ctx := setupWithPassword(t)

	token, _, err := service.Login(ctx, "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	// Move the expiration near so validation refreshes it. Fresh sessions
	// hold nearly the full duration and validate read-only.
	near := util.Now().Add(time.Hour)
	if _, err := gorm.G[record.Session](database.Cache, clause.Eq{Column: "token_hash", Value: util.HashSecret(token)}).Update(ctx, "expires_at", near); err != nil {
		t.Fatalf("move expiration near failed: %v", err)
	}

	session, valid := service.ValidateSession(ctx, token)
	if !valid {
		t.Fatal("session should be valid")
	}
	if !session.Refreshed || !session.ExpiresAt.After(near) {
		t.Errorf("validate should extend the session: got %+v", session)
	}
	after, err := gorm.G[record.Session](database.Cache, clause.Eq{Column: "token_hash", Value: util.HashSecret(token)}).First(ctx)
	if err != nil {
		t.Fatalf("get refreshed session failed: %v", err)
	}
	if !after.ExpiresAt.After(near) {
		t.Error("validate should persist the extended expiration")
	}
}

func TestGormService_ValidateSession_Concurrent(t *testing.T) {
	service, _, ctx := setupWithPassword(t)

	token, _, err := service.Login(ctx, "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	const callers = 16
	var group sync.WaitGroup
	rejected := make(chan struct{}, callers)
	for i := 0; i < callers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, valid := service.ValidateSession(ctx, token); !valid {
				rejected <- struct{}{}
			}
		}()
	}
	group.Wait()
	close(rejected)
	if count := len(rejected); count > 0 {
		t.Errorf("concurrent validate rejected the session %d times", count)
	}
}

func TestGormService_ValidateSession_ExpiredDeletesRow(t *testing.T) {
	service, database, ctx := setupWithPassword(t)

	token, _, err := service.Login(ctx, "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	past := util.Now().Add(-time.Hour)
	if _, err := gorm.G[record.Session](database.Cache, clause.Eq{Column: "token_hash", Value: util.HashSecret(token)}).Update(ctx, "expires_at", past); err != nil {
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
	service, _, ctx := setupWithPassword(t)

	token, _, err := service.Login(ctx, "password123", false)
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
	service, database, ctx := setupWithPassword(t)

	kept, _, err := service.Login(ctx, "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	expired, _, err := service.Login(ctx, "password123", false)
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
	service, _, ctx := setupWithPassword(t)

	if err := service.ChangePassword(ctx, "password123", "password456"); err != nil {
		t.Fatalf("change password failed: %v", err)
	}
	if _, _, err := service.Login(ctx, "password123", false); err != ErrInvalidCredentials {
		t.Errorf("old password: want ErrInvalidCredentials, got %v", err)
	}
	if _, _, err := service.Login(ctx, "password456", false); err != nil {
		t.Errorf("new password login failed: %v", err)
	}
}

func TestGormService_ChangePassword_InvalidCurrent(t *testing.T) {
	service, _, ctx := setupWithPassword(t)

	if err := service.ChangePassword(ctx, "wrongpassword", "password456"); err != ErrInvalidCredentials {
		t.Errorf("wrong current password: want ErrInvalidCredentials, got %v", err)
	}
	if err := service.ChangePassword(ctx, "password123", ""); !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Errorf("empty new password: want invalid-input, got %v", err)
	}
}
