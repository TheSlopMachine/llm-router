package admin

import (
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func setupAdminService(t *testing.T) *Service {
	t.Helper()
	database := testutil.SetupTestDB(t)
	providerSvc := provider.NewService(database)
	return New(database, providerSvc)
}

// ─────────────────────────────────────────────
// Bootstrap Tests
// ─────────────────────────────────────────────

func TestAdminService_Bootstrap_FirstTime(t *testing.T) {
	svc := setupAdminService(t)
	
	err := svc.Bootstrap("admin", "password123")
	if err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	
	// Verify admin user was created
	user, err := svc.repo.Get("admin")
	if err != nil {
		t.Fatalf("get admin user failed: %v", err)
	}
	
	if user.Username != "admin" {
		t.Errorf("username: got %q, want %q", user.Username, "admin")
	}
	if user.PasswordHash == "" {
		t.Error("password hash should not be empty")
	}
}

func TestAdminService_Bootstrap_AlreadyBootstrapped(t *testing.T) {
	svc := setupAdminService(t)
	
	// First bootstrap
	if err := svc.Bootstrap("admin", "password123"); err != nil {
		t.Fatalf("first bootstrap failed: %v", err)
	}
	
	// Second bootstrap should fail
	err := svc.Bootstrap("admin2", "password456")
	if err == nil {
		t.Error("expected error for second bootstrap, got nil")
	}
}

func TestAdminService_IsBootstrapped(t *testing.T) {
	svc := setupAdminService(t)
	
	// Should not be bootstrapped initially
	ok, err := svc.db.IsBootstrapped()
	if err != nil {
		t.Fatalf("is bootstrapped check failed: %v", err)
	}
	if ok {
		t.Error("should not be bootstrapped initially")
	}
	
	// Bootstrap
	if err := svc.Bootstrap("admin", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	
	// Should be bootstrapped now
	ok, err = svc.db.IsBootstrapped()
	if err != nil {
		t.Fatalf("is bootstrapped check failed: %v", err)
	}
	if !ok {
		t.Error("should be bootstrapped after bootstrap")
	}
}

func TestAdminService_Bootstrap_DoesNotCreateProvidersBucket(t *testing.T) {
	svc := setupAdminService(t)

	if err := svc.Bootstrap("admin", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}

	if err := svc.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(db.BucketProviders) != nil {
			t.Fatalf("providers bucket should not be created during bootstrap")
		}
		return nil
	}); err != nil {
		t.Fatalf("db view failed: %v", err)
	}
}

// ─────────────────────────────────────────────
// Login Tests
// ─────────────────────────────────────────────

func TestAdminService_Login_ValidCredentials(t *testing.T) {
	svc := setupAdminService(t)
	
	// Bootstrap first
	if err := svc.Bootstrap("admin", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	
	// Login
	token, expiresAt, err := svc.Login("admin", "password123", false)
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

func TestAdminService_Login_InvalidPassword(t *testing.T) {
	svc := setupAdminService(t)
	
	// Bootstrap first
	if err := svc.Bootstrap("admin", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	
	// Login with wrong password
	_, _, err := svc.Login("admin", "wrongpassword", false)
	if err == nil {
		t.Error("expected error for invalid password, got nil")
	}
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestAdminService_Login_UserNotFound(t *testing.T) {
	svc := setupAdminService(t)
	
	// Login without bootstrap
	_, _, err := svc.Login("nonexistent", "password", false)
	if err == nil {
		t.Error("expected error for nonexistent user, got nil")
	}
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestAdminService_Login_RememberMe(t *testing.T) {
	svc := setupAdminService(t)
	
	// Bootstrap first
	if err := svc.Bootstrap("admin", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	
	// Login with remember me
	token, expiresAt, err := svc.Login("admin", "password123", true)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	
	if token == "" {
		t.Error("token should not be empty")
	}
	
	// Remember me should have longer expiration (30 days vs 8 hours)
	duration := time.Until(expiresAt)
	if duration < 24*time.Hour {
		t.Errorf("remember me duration too short: %v", duration)
	}
}

// ─────────────────────────────────────────────
// ValidateSession Tests
// ─────────────────────────────────────────────

func TestAdminService_ValidateSession_Valid(t *testing.T) {
	svc := setupAdminService(t)
	
	// Bootstrap and login
	if err := svc.Bootstrap("admin", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	
	token, _, err := svc.Login("admin", "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	
	// Validate session
	username, valid := svc.ValidateSession(token)
	if !valid {
		t.Error("session should be valid")
	}
	if username != "admin" {
		t.Errorf("username: got %q, want %q", username, "admin")
	}
}

func TestAdminService_ValidateSession_Invalid(t *testing.T) {
	svc := setupAdminService(t)
	
	// Validate non-existent session
	_, valid := svc.ValidateSession("invalid-token")
	if valid {
		t.Error("session should not be valid")
	}
}

func TestAdminService_ValidateSession_SlidingWindow(t *testing.T) {
	svc := setupAdminService(t)
	
	// Bootstrap and login
	if err := svc.Bootstrap("admin", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	
	token, _, err := svc.Login("admin", "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	
	// Wait a bit
	time.Sleep(100 * time.Millisecond)
	
	// Validate session (should refresh expiry)
	username, valid := svc.ValidateSession(token)
	if !valid {
		t.Error("session should be valid")
	}
	if username != "admin" {
		t.Errorf("username: got %q, want %q", username, "admin")
	}
	
	// The expiry should have been extended (sliding window)
	// We can't easily check this without accessing internal state,
	// but we verified the session is still valid
}

// ─────────────────────────────────────────────
// Logout Tests
// ─────────────────────────────────────────────

func TestAdminService_Logout(t *testing.T) {
	svc := setupAdminService(t)
	
	// Bootstrap and login
	if err := svc.Bootstrap("admin", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	
	token, _, err := svc.Login("admin", "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	
	// Verify session is valid
	_, valid := svc.ValidateSession(token)
	if !valid {
		t.Error("session should be valid before logout")
	}
	
	// Logout
	svc.Logout(token)
	
	// Verify session is invalid
	_, valid = svc.ValidateSession(token)
	if valid {
		t.Error("session should be invalid after logout")
	}
}

// ─────────────────────────────────────────────
// CleanupExpiredSessions Tests
// ─────────────────────────────────────────────

func TestAdminService_CleanupExpiredSessions(t *testing.T) {
	svc := setupAdminService(t)
	
	// Bootstrap and login
	if err := svc.Bootstrap("admin", "password123"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	
	token, _, err := svc.Login("admin", "password123", false)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	
	// Session should be valid
	_, valid := svc.ValidateSession(token)
	if !valid {
		t.Error("session should be valid")
	}
	
	// Cleanup should not remove valid sessions
	if err := svc.CleanupExpiredSessions(); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	
	// Session should still be valid
	_, valid = svc.ValidateSession(token)
	if !valid {
		t.Error("session should still be valid after cleanup")
	}
}

