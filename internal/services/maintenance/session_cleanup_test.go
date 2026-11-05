package maintenance

import (
	"context"
	"testing"
	"time"
)

func TestCleanupSessionsRunsOncePerInterval(t *testing.T) {
	service := NewGormService(nil, nil, nil)
	calls := 0
	service.SetSessionCleanup(func(context.Context) error {
		calls++
		return nil
	})
	start := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	service.cleanupSessions(context.Background(), start)
	service.cleanupSessions(context.Background(), start.Add(30*time.Minute))
	if calls != 1 {
		t.Fatalf("cleanup inside the interval must skip, calls = %d", calls)
	}
	service.cleanupSessions(context.Background(), start.Add(sessionCleanupInterval))
	if calls != 2 {
		t.Fatalf("cleanup after the interval must run, calls = %d", calls)
	}
}
