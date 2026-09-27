package maintenance

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// staleStack seeds one provider of type "stale-type" with n stale
// credentials. The adapter's refresh behavior comes from the caller.
func staleStack(t *testing.T, adapter *testutil.MockAdapter, n int) (*Service, *credential.Service, []string) {
	t.Helper()
	database := testutil.SetupTestDB(t)
	providerSvc := provider.NewService(database)
	providerSvc.RegisterGoAdapter(adapter)

	inst, err := providerSvc.Create(provider.CreateOptions{Name: "Stale", TypeKey: "stale-type"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	credSvc := credential.New(database, providerSvc)
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		cred, err := credSvc.Add(credential.AddOptions{
			ProviderID: inst.ID,
			Label:      "stale-cred",
			Data:       map[string]any{"api_key": "stale-key"},
		})
		if err != nil {
			t.Fatalf("add credential: %v", err)
		}
		ids = append(ids, cred.ID)
	}
	svc := New(credSvc, providerSvc, database, slog.Default())
	return svc, credSvc, ids
}

// TestRunStartupRefresh_RefreshesStaleCredentials proves the startup gate:
// stale credentials are refreshed synchronously before the server listens,
// not on the first ticker tick.
func TestRunStartupRefresh_RefreshesStaleCredentials(t *testing.T) {
	var calls atomic.Int32
	adapter := testutil.NewMockAdapter("stale-type").
		WithNeedsRefreshFunc(func(*models.Credential) bool { return true }).
		WithRefreshFunc(func(_ context.Context, _ *models.Credential) (map[string]any, error) {
			calls.Add(1)
			return map[string]any{"api_key": "fresh-key"}, nil
		})
	svc, credSvc, ids := staleStack(t, adapter, 3)

	svc.RunStartupRefresh(context.Background())

	if got := calls.Load(); got != 3 {
		t.Fatalf("refreshed %d credentials, want 3", got)
	}
	for _, id := range ids {
		cred, err := credSvc.Get(id)
		if err != nil {
			t.Fatalf("get credential: %v", err)
		}
		if cred.Data["api_key"] != "fresh-key" {
			t.Fatalf("credential %s not persisted refreshed: %v", id, cred.Data)
		}
	}
}

// TestRefreshStaleCredentials_RunsConcurrently proves workers overlap: each
// refresh blocks at a barrier until every worker has entered. A serial loop
// would park the first worker there forever, so the test fails bounded
// instead of hanging.
func TestRefreshStaleCredentials_RunsConcurrently(t *testing.T) {
	const n = 3
	entered := make(chan struct{}, n)
	release := make(chan struct{})
	adapter := testutil.NewMockAdapter("stale-type").
		WithNeedsRefreshFunc(func(*models.Credential) bool { return true }).
		WithRefreshFunc(func(_ context.Context, _ *models.Credential) (map[string]any, error) {
			entered <- struct{}{}
			select {
			case <-release:
			case <-time.After(5 * time.Second):
				return nil, context.DeadlineExceeded
			}
			return map[string]any{"api_key": "fresh-key"}, nil
		})
	svc, _, _ := staleStack(t, adapter, n)

	done := make(chan int, 1)
	go func() {
		done <- svc.refreshStaleCredentials(context.Background())
	}()
	for i := 0; i < n; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d workers entered the refresh together: pool is serial", i, n)
		}
	}
	close(release)
	select {
	case got := <-done:
		if got != n {
			t.Fatalf("refreshed %d credentials, want %d", got, n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("refresh pool did not finish after release")
	}
}

// TestRefreshStaleCredentials_BoundsWorkers proves the pool never exceeds
// maxRefreshWorkers no matter how many credentials are stale.
func TestRefreshStaleCredentials_BoundsWorkers(t *testing.T) {
	const n = 8
	var current, max atomic.Int32
	var calls atomic.Int32
	adapter := testutil.NewMockAdapter("stale-type").
		WithNeedsRefreshFunc(func(*models.Credential) bool { return true }).
		WithRefreshFunc(func(_ context.Context, _ *models.Credential) (map[string]any, error) {
			c := current.Add(1)
			for {
				m := max.Load()
				if c <= m || max.CompareAndSwap(m, c) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			current.Add(-1)
			calls.Add(1)
			return map[string]any{"api_key": "fresh-key"}, nil
		})
	svc, _, _ := staleStack(t, adapter, n)

	if got := svc.refreshStaleCredentials(context.Background()); got != n {
		t.Fatalf("refreshed %d credentials, want %d", got, n)
	}
	if got := calls.Load(); got != n {
		t.Fatalf("refresh ran %d times, want %d", got, n)
	}
	if got := max.Load(); got > maxRefreshWorkers {
		t.Fatalf("peak %d concurrent refreshes exceeds limit %d", got, maxRefreshWorkers)
	}
}

// TestStart_TicksRefresh proves the background loop still refreshes on the
// ticker after the startup gate moved to RunStartupRefresh.
func TestStart_TicksRefresh(t *testing.T) {
	refreshed := make(chan struct{}, 1)
	adapter := testutil.NewMockAdapter("stale-type").
		WithNeedsRefreshFunc(func(*models.Credential) bool { return true }).
		WithRefreshFunc(func(_ context.Context, _ *models.Credential) (map[string]any, error) {
			select {
			case refreshed <- struct{}{}:
			default:
			}
			return map[string]any{"api_key": "fresh-key"}, nil
		})
	svc, _, _ := staleStack(t, adapter, 1)
	svc.WithInterval(50 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)

	select {
	case <-refreshed:
	case <-time.After(5 * time.Second):
		t.Fatal("credential was not refreshed on the ticker")
	}
}

// TestRunStartupRefresh_RespectsCancellation proves a cancelled gate stops
// launching workers instead of refreshing the whole pool while shutting down.
func TestRunStartupRefresh_RespectsCancellation(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	adapter := testutil.NewMockAdapter("stale-type").
		WithNeedsRefreshFunc(func(*models.Credential) bool { return true }).
		WithRefreshFunc(func(ctx context.Context, _ *models.Credential) (map[string]any, error) {
			calls.Add(1)
			select {
			case <-release:
				return map[string]any{"api_key": "fresh-key"}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})
	svc, _, _ := staleStack(t, adapter, maxRefreshWorkers+2)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		svc.RunStartupRefresh(ctx)
	}()
	// Let the first wave of workers start, then cancel: late credentials
	// must never launch.
	time.Sleep(200 * time.Millisecond)
	cancel()
	close(release)
	wg.Wait()
	if got := calls.Load(); got > maxRefreshWorkers {
		t.Fatalf("cancelled gate launched %d refreshes, want at most %d", got, maxRefreshWorkers)
	}
}
