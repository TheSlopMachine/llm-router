package pool

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// fakeTracker records usage calls for pool tests.
type fakeTracker struct {
	success  map[string]int
	failure  map[string]int
	usageErr error
}

func newFakeTracker() *fakeTracker {
	return &fakeTracker{
		success: map[string]int{},
		failure: map[string]int{},
	}
}

func (f *fakeTracker) UpdateUsage(id string, success bool) error {
	if f.usageErr != nil {
		return f.usageErr
	}
	if success {
		f.success[id]++
	} else {
		f.failure[id]++
	}
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

var errFatal = errors.New("fatal: no handler")

func TestRunFirstSuccess(t *testing.T) {
	tracker := newFakeTracker()
	calls := 0
	resp, err := Run(context.Background(), nil, poolCreds("a", "b"), tracker,
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
			calls++
			return okResp()
		}, nil)
	if err != nil || resp.ID != "ok" || calls != 1 {
		t.Fatalf("got resp=%v err=%v calls=%d", resp, err, calls)
	}
	if tracker.success["a"] != 1 {
		t.Errorf("expected success tracked for a, got %v", tracker.success)
	}
}

func TestRunTriesEachKeyOnceInOrder(t *testing.T) {
	tracker := newFakeTracker()
	var order []string
	resp, err := Run(context.Background(), nil, poolCreds("a", "b", "c"), tracker,
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
			order = append(order, cred.ID)
			if cred.ID != "c" {
				return nil, &models.ProviderError{StatusCode: 429, Type: models.ErrorTypeRateLimit, Message: "limited"}
			}
			return okResp()
		}, nil)
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

func TestRunReturnsLastError(t *testing.T) {
	calls := 0
	_, err := Run(context.Background(), nil, poolCreds("a", "b"), nil,
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
			calls++
			return nil, &models.ProviderError{StatusCode: 429, Type: models.ErrorTypeRateLimit, Message: "fail-" + cred.ID}
		}, nil)
	if err == nil || calls != 2 {
		t.Fatalf("got err=%v calls=%d", err, calls)
	}
	if err.Error() != "provider error (429): fail-b" {
		t.Errorf("expected last error, got %v", err)
	}
}

func TestRunEmpty(t *testing.T) {
	if _, err := Run(context.Background(), nil, nil, nil,
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
			return okResp()
		}, nil); !errors.Is(err, NoCredentials) {
		t.Fatalf("expected NoCredentials, got %v", err)
	}
}

func TestRunFatalStopsImmediately(t *testing.T) {
	calls := 0
	_, err := Run(context.Background(), nil, poolCreds("a", "b"), nil,
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
			calls++
			return nil, errFatal
		}, func(err error) bool { return errors.Is(err, errFatal) })
	if !errors.Is(err, errFatal) {
		t.Fatalf("expected fatal error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("fatal error must stop after 1 attempt, got %d", calls)
	}
}

func TestRunFailureTracksUsageOnly(t *testing.T) {
	tracker := newFakeTracker()
	_, _ = Run(context.Background(), nil, poolCreds("a"), tracker,
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
			return nil, &models.ProviderError{StatusCode: 429, Type: models.ErrorTypeQuotaExceeded, Message: "quota"}
		}, nil)
	if tracker.failure["a"] != 1 {
		t.Errorf("failure: got %v", tracker.failure)
	}
}

func TestRunCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, err := Run(ctx, nil, poolCreds("a", "b"), nil,
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
			calls++
			return okResp()
		}, nil)
	if err == nil || calls != 0 {
		t.Fatalf("got err=%v calls=%d", err, calls)
	}
}

func TestRunTrackingErrorsDoNotFailAttempts(t *testing.T) {
	tracker := newFakeTracker()
	tracker.usageErr = errors.New("storage unavailable")
	resp, err := Run(context.Background(), slog.Default(), poolCreds("a"), tracker,
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
			return okResp()
		}, nil)
	if err != nil || resp.ID != "ok" {
		t.Fatalf("tracking failure must not fail the attempt: resp=%v err=%v", resp, err)
	}
}

func TestRunStreamFallsThrough(t *testing.T) {
	calls := 0
	err := RunStream(context.Background(), nil, io.Discard, poolCreds("a", "b"), nil,
		func(ctx context.Context, cred *models.Credential, w io.Writer) error {
			calls++
			if cred.ID == "a" {
				return &models.ProviderError{StatusCode: 429, Type: models.ErrorTypeRateLimit, Message: "limited"}
			}
			_, _ = io.WriteString(w, "data: ok\n\n")
			return nil
		}, nil)
	if err != nil || calls != 2 {
		t.Fatalf("got err=%v calls=%d", err, calls)
	}
}

func TestRunStreamStopsAfterFirstByte(t *testing.T) {
	calls := 0
	err := RunStream(context.Background(), nil, io.Discard, poolCreds("a", "b"), nil,
		func(ctx context.Context, cred *models.Credential, w io.Writer) error {
			calls++
			_, _ = io.WriteString(w, "data: partial\n\n")
			return &models.ProviderError{StatusCode: 500, Type: models.ErrorTypeUpstream, Message: "died mid-stream"}
		}, nil)
	if err == nil || calls != 1 {
		t.Fatalf("stream must stop after first byte: err=%v calls=%d", err, calls)
	}
}

func TestRunStreamEmpty(t *testing.T) {
	err := RunStream(context.Background(), nil, io.Discard, nil, nil,
		func(ctx context.Context, cred *models.Credential, w io.Writer) error { return nil }, nil)
	if !errors.Is(err, NoCredentials) {
		t.Fatalf("expected NoCredentials, got %v", err)
	}
}
