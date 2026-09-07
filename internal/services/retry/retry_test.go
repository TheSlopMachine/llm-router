package retry

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestClassify(t *testing.T) {
	retryAfter := time.Now().Add(time.Minute)
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"rate limit", &models.ProviderError{Type: models.ErrorTypeRateLimit}, true},
		{"quota", &models.ProviderError{Type: models.ErrorTypeQuotaExceeded, RetryAfter: &retryAfter}, true},
		{"auth", &models.ProviderError{Type: models.ErrorTypeAuth}, false},
		{"upstream", &models.ProviderError{Type: models.ErrorTypeUpstream}, false},
		{"timeout", &models.ProviderError{Type: models.ErrorTypeTimeout}, false},
		{"plugin internal", &models.PluginInternalError{PluginID: "x", Cause: "boom"}, true},
		{"plain", fmt.Errorf("plain"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.err); got != tc.want {
				t.Errorf("Classify: got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRunFirstSuccess(t *testing.T) {
	calls := 0
	candidates := []Candidate[string]{
		{Label: "a", Run: func(ctx context.Context) (string, error) { calls++; return "ok", nil }},
		{Label: "b", Run: func(ctx context.Context) (string, error) { calls++; return "", fmt.Errorf("unreached") }},
	}
	res, err := Run(context.Background(), candidates, slog.Default())
	if err != nil || res != "ok" || calls != 1 {
		t.Fatalf("got res=%q err=%v calls=%d", res, err, calls)
	}
}

func TestRunRetryableFallsThrough(t *testing.T) {
	candidates := []Candidate[string]{
		{Label: "a", Run: func(ctx context.Context) (string, error) {
			return "", &models.ProviderError{Type: models.ErrorTypeRateLimit, Message: "limited"}
		}},
		{Label: "b", Run: func(ctx context.Context) (string, error) { return "recovered", nil }},
	}
	res, err := Run(context.Background(), candidates, slog.Default())
	if err != nil || res != "recovered" {
		t.Fatalf("got res=%q err=%v", res, err)
	}
}

func TestRunTerminalStops(t *testing.T) {
	calls := 0
	candidates := []Candidate[string]{
		{Label: "a", Run: func(ctx context.Context) (string, error) {
			calls++
			return "", &models.ProviderError{Type: models.ErrorTypeAuth, Message: "bad key"}
		}},
		{Label: "b", Run: func(ctx context.Context) (string, error) { calls++; return "unreached", nil }},
	}
	if _, err := Run(context.Background(), candidates, slog.Default()); err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("terminal error must stop after 1 call, got %d", calls)
	}
}

func TestRunAllExhausted(t *testing.T) {
	candidates := []Candidate[string]{
		{Label: "a", Run: func(ctx context.Context) (string, error) {
			return "", &models.ProviderError{Type: models.ErrorTypeRateLimit, Message: "limited"}
		}},
		{Label: "b", Run: func(ctx context.Context) (string, error) {
			return "", &models.PluginInternalError{PluginID: "p", Cause: "crash"}
		}},
	}
	_, err := Run(context.Background(), candidates, slog.Default())
	if err == nil {
		t.Fatal("expected exhausted error")
	}
}

func TestRunEmpty(t *testing.T) {
	if _, err := Run[string](context.Background(), nil, slog.Default()); err == nil {
		t.Fatal("expected error for empty candidates")
	}
}

func TestRunStreamFallsThrough(t *testing.T) {
	candidates := []StreamCandidate{
		{Label: "a", Run: func(ctx context.Context, w io.Writer) error {
			return &models.ProviderError{Type: models.ErrorTypeQuotaExceeded, Message: "quota"}
		}},
		{Label: "b", Run: func(ctx context.Context, w io.Writer) error {
			_, _ = io.WriteString(w, "data: ok\n\n")
			return nil
		}},
	}
	if err := RunStream(context.Background(), candidates, io.Discard, slog.Default()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
