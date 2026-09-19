package luaplugin

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// fakeTracker records usage calls for pool tests.
type fakeTracker struct {
	success map[string]int
	failure map[string]int
	quota   map[string]time.Time
}

func newFakeTracker() *fakeTracker {
	return &fakeTracker{
		success: map[string]int{},
		failure: map[string]int{},
		quota:   map[string]time.Time{},
	}
}

func (f *fakeTracker) UpdateUsage(id string, success bool) error {
	if success {
		f.success[id]++
	} else {
		f.failure[id]++
	}
	return nil
}

func (f *fakeTracker) MarkQuotaExceeded(id string, resetAt time.Time) error {
	f.quota[id] = resetAt
	return nil
}

func poolCreds(ids ...string) []*models.Credential {
	creds := make([]*models.Credential, 0, len(ids))
	for _, id := range ids {
		creds = append(creds, &models.Credential{ID: id, ProviderID: "p"})
	}
	return creds
}

func okResp() (*models.ChatCompletionResponse, error) {
	return &models.ChatCompletionResponse{ID: "ok"}, nil
}

func TestRunPoolFirstSuccess(t *testing.T) {
	tracker := newFakeTracker()
	svc := &Service{usage: tracker}
	calls := 0
	resp, err := runPool(context.Background(), svc, poolCreds("a", "b"),
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
			calls++
			return okResp()
		})
	if err != nil || resp.ID != "ok" || calls != 1 {
		t.Fatalf("got resp=%v err=%v calls=%d", resp, err, calls)
	}
	if tracker.success["a"] != 1 {
		t.Errorf("expected success tracked for a, got %v", tracker.success)
	}
}

func TestRunPoolTriesEachKeyOnceInOrder(t *testing.T) {
	tracker := newFakeTracker()
	svc := &Service{usage: tracker}
	var order []string
	resp, err := runPool(context.Background(), svc, poolCreds("a", "b", "c"),
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
			order = append(order, cred.ID)
			if cred.ID != "c" {
				return nil, &models.ProviderError{StatusCode: 429, Type: models.ErrorTypeRateLimit, Message: "limited"}
			}
			return okResp()
		})
	if err != nil || resp.ID != "ok" {
		t.Fatalf("got resp=%v err=%v", resp, err)
	}
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Fatalf("order: got %v", order)
	}
	if tracker.failure["a"] != 1 || tracker.failure["b"] != 1 || tracker.success["c"] != 1 {
		t.Errorf("usage: success=%v failure=%v", tracker.success, tracker.failure)
	}
}

func TestRunPoolReturnsLastError(t *testing.T) {
	svc := &Service{}
	calls := 0
	_, err := runPool(context.Background(), svc, poolCreds("a", "b"),
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
			calls++
			return nil, &models.ProviderError{StatusCode: 429, Type: models.ErrorTypeRateLimit, Message: "fail-" + cred.ID}
		})
	if err == nil || calls != 2 {
		t.Fatalf("got err=%v calls=%d", err, calls)
	}
	if err.Error() != "provider error (429): fail-b" {
		t.Errorf("expected last error, got %v", err)
	}
}

func TestRunPoolEmpty(t *testing.T) {
	svc := &Service{}
	if _, err := runPool(context.Background(), svc, nil,
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
			return okResp()
		}); err == nil {
		t.Fatal("expected error for empty pool, got nil")
	}
}

func TestRunPoolMissingHandlerStopsImmediately(t *testing.T) {
	svc := &Service{}
	calls := 0
	_, err := runPool(context.Background(), svc, poolCreds("a", "b"),
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
			calls++
			return nil, &notFoundError{PluginID: "p", TypeKey: "t", Handler: "complete"}
		})
	if !errors.Is(err, ErrHandlerNotFound) {
		t.Fatalf("expected ErrHandlerNotFound, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("missing handler must stop after 1 attempt, got %d", calls)
	}
}

func TestRunPoolMarksQuotaExceeded(t *testing.T) {
	tracker := newFakeTracker()
	svc := &Service{usage: tracker}
	resetAt := time.Now().Add(time.Hour).Truncate(time.Second)
	_, _ = runPool(context.Background(), svc, poolCreds("a"),
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
			return nil, &models.ProviderError{StatusCode: 429, Type: models.ErrorTypeQuotaExceeded, Message: "quota", RetryAfter: &resetAt}
		})
	if got := tracker.quota["a"].Truncate(time.Second); !got.Equal(resetAt) {
		t.Errorf("quota mark: got %v, want %v", got, resetAt)
	}
	if tracker.failure["a"] != 1 {
		t.Errorf("failure: got %v", tracker.failure)
	}
}

func TestRunPoolCanceledContext(t *testing.T) {
	svc := &Service{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, err := runPool(ctx, svc, poolCreds("a", "b"),
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
			calls++
			return okResp()
		})
	if err == nil || calls != 0 {
		t.Fatalf("got err=%v calls=%d", err, calls)
	}
}

func TestRunPoolStreamFallsThrough(t *testing.T) {
	svc := &Service{}
	calls := 0
	err := svc.runPoolStream(context.Background(), io.Discard, poolCreds("a", "b"),
		func(ctx context.Context, cred *models.Credential, w io.Writer) error {
			calls++
			if cred.ID == "a" {
				return &models.ProviderError{StatusCode: 429, Type: models.ErrorTypeRateLimit, Message: "limited"}
			}
			_, _ = io.WriteString(w, "data: ok\n\n")
			return nil
		})
	if err != nil || calls != 2 {
		t.Fatalf("got err=%v calls=%d", err, calls)
	}
}
