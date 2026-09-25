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

// captureHandler records slog records for proxy attr assertions.
type captureHandler struct {
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *captureHandler) WithGroup(string) slog.Handler { return h }

func recordProxy(r slog.Record) (string, bool) {
	found := ""
	ok := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "proxy" {
			found = a.Value.String()
			ok = true
		}
		return true
	})
	return found, ok
}

func TestRunWithProxy_ReturnsLastProxy(t *testing.T) {
	_, proxy, err := RunWithProxy(context.Background(), nil, poolCreds("a", "b"), nil,
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, string, error) {
			return nil, "proxy-" + cred.ID + ":8080", &models.ProviderError{StatusCode: 429, Type: models.ErrorTypeRateLimit, Message: "limited"}
		}, nil)
	if err == nil {
		t.Fatal("expected failure")
	}
	if proxy != "proxy-b:8080" {
		t.Fatalf("last proxy: got %q want %q", proxy, "proxy-b:8080")
	}
}

func TestRunWithProxy_LogsProxyPerAttempt(t *testing.T) {
	h := &captureHandler{}
	log := slog.New(h)
	_, _, _ = RunWithProxy(context.Background(), log, poolCreds("a", "b"), nil,
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, string, error) {
			return nil, "10.0.0.1:8080", &models.ProviderError{StatusCode: 429, Type: models.ErrorTypeRateLimit, Message: "limited"}
		}, nil)
	if len(h.records) != 2 {
		t.Fatalf("expected trying-next plus all-failed, got %d records", len(h.records))
	}
	for _, r := range h.records {
		got, ok := recordProxy(r)
		if !ok || got != "10.0.0.1:8080" {
			t.Fatalf("record %q missing proxy attr: %+v", r.Message, h.records)
		}
	}
}

func TestRunWithProxy_OmitsProxyWhenDirect(t *testing.T) {
	h := &captureHandler{}
	log := slog.New(h)
	_, _, _ = RunWithProxy(context.Background(), log, poolCreds("a", "b"), nil,
		func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, string, error) {
			return nil, "", &models.ProviderError{StatusCode: 429, Type: models.ErrorTypeRateLimit, Message: "limited"}
		}, nil)
	for _, r := range h.records {
		if _, ok := recordProxy(r); ok {
			t.Fatalf("direct request must omit proxy attr: %+v", h.records)
		}
	}
}

func TestRunStreamWithProxy_ReturnsLastProxy(t *testing.T) {
	proxy, err := RunStreamWithProxy(context.Background(), nil, io.Discard, poolCreds("a", "b"), nil,
		func(ctx context.Context, cred *models.Credential, w io.Writer) (string, error) {
			return "proxy-" + cred.ID + ":8080", &models.ProviderError{StatusCode: 429, Type: models.ErrorTypeRateLimit, Message: "limited"}
		}, nil)
	if err == nil {
		t.Fatal("expected failure")
	}
	if proxy != "proxy-b:8080" {
		t.Fatalf("last proxy: got %q want %q", proxy, "proxy-b:8080")
	}
}
