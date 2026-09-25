package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func render(t *testing.T, level slog.Level, attrs []slog.Attr, msg string, ts time.Time) string {
	t.Helper()
	var buf bytes.Buffer
	h := NewHandler(&buf, slog.LevelDebug)
	r := slog.NewRecord(ts, level, msg, 0)
	r.AddAttrs(attrs...)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	return buf.String()
}

func TestFormat(t *testing.T) {
	ts := time.Date(2026, 9, 25, 12, 22, 15, 716*1e6, time.Local)
	got := render(t, slog.LevelInfo, []slog.Attr{
		slog.String("virtual_model", "gemini-flashes"),
		slog.String("model", "google/gemini-3.5-flash"),
	}, "member model failed, trying next", ts)
	want := "[INFO][25.09.2026 12:22:15.716][virtual_model: gemini-flashes][model: google/gemini-3.5-flash] member model failed, trying next\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNoAttrs(t *testing.T) {
	ts := time.Date(2026, 9, 25, 12, 22, 15, 0, time.Local)
	got := render(t, slog.LevelWarn, nil, "all credentials failed", ts)
	if strings.Contains(got, "[model:") {
		t.Fatalf("unexpected attr bracket in %q", got)
	}
	if !strings.HasPrefix(got, "[WARN][25.09.2026 12:22:15.000] all credentials failed") {
		t.Fatalf("unexpected line %q", got)
	}
}

func TestWithAndGroup(t *testing.T) {
	var buf bytes.Buffer
	h := NewHandler(&buf, slog.LevelDebug)
	h = h.WithAttrs([]slog.Attr{slog.String("model", "m1")}).(*Handler)
	h2 := h.WithGroup("g")
	h2 = h2.WithAttrs([]slog.Attr{slog.String("k", "v")}).(slog.Handler)
	r := slog.NewRecord(time.Date(2026, 9, 25, 1, 2, 3, 0, time.Local), slog.LevelInfo, "msg", 0)
	if err := h2.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "[model: m1]") || !strings.Contains(got, "[g.k: v]") {
		t.Fatalf("unexpected line %q", got)
	}
}

func TestEnabled(t *testing.T) {
	var buf bytes.Buffer
	h := NewHandler(&buf, slog.LevelInfo)
	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("info must pass at info level")
	}
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("debug must not pass at info level")
	}
}

func TestErrorValue(t *testing.T) {
	ts := time.Date(2026, 9, 25, 12, 0, 0, 0, time.Local)
	got := render(t, slog.LevelInfo, []slog.Attr{
		slog.Any("error", errTest{}),
	}, "fail", ts)
	if !strings.Contains(got, "[error: boom]") {
		t.Fatalf("unexpected line %q", got)
	}
}

type errTest struct{}

func (errTest) Error() string { return "boom" }
