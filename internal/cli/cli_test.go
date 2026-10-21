package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSeverity(t *testing.T) {
	cases := map[string]int{"debug": 0, "INFO": 1, "warn": 2, "warning": 2, "error": 3, "fatal": 4, "3": 3, " 1 ": 1}
	for raw, want := range cases {
		if got, err := ParseSeverity(raw); err != nil || got != want {
			t.Errorf("ParseSeverity(%q) = %d, %v, want %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "loud", "5", "-1"} {
		if _, err := ParseSeverity(raw); err == nil {
			t.Errorf("ParseSeverity(%q) succeeded", raw)
		}
	}
}

func TestParseTimeBound(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"30m":                  now.Add(-30 * time.Minute),
		"6h":                   now.Add(-6 * time.Hour),
		"2d":                   now.Add(-48 * time.Hour),
		"2026-10-20T09:00:00Z": time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC),
	}
	for raw, want := range cases {
		if got, err := ParseTimeBound(raw, now); err != nil || !got.Equal(want) {
			t.Errorf("ParseTimeBound(%q) = %v, %v, want %v", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "yesterday", "-1h", "-2d", "1x"} {
		if _, err := ParseTimeBound(raw, now); err == nil {
			t.Errorf("ParseTimeBound(%q) succeeded", raw)
		}
	}
}

func TestParseKeyFilter(t *testing.T) {
	key, value, err := ParseKeyFilter("code=rate_limit")
	if err != nil || key != "code" || value == nil || *value != "rate_limit" {
		t.Errorf("key=value: %q, %v, %v", key, value, err)
	}
	key, value, err = ParseKeyFilter("token_id")
	if err != nil || key != "token_id" || value != nil {
		t.Errorf("bare key: %q, %v, %v", key, value, err)
	}
	key, value, err = ParseKeyFilter("msg=a=b")
	if err != nil || key != "msg" || *value != "a=b" {
		t.Errorf("value with equals: %q, %v, %v", key, value, err)
	}
	if _, _, err := ParseKeyFilter("=x"); err == nil {
		t.Error("empty key succeeded")
	}
}

func TestReadPassword(t *testing.T) {
	if got, err := ReadPassword(strings.NewReader("secret\r\n")); err != nil || got != "secret" {
		t.Errorf("ReadPassword = %q, %v", got, err)
	}
	if got, err := ReadPassword(strings.NewReader(" a b ")); err != nil || got != " a b " {
		t.Errorf("inner spaces kept: %q, %v", got, err)
	}
	if _, err := ReadPassword(strings.NewReader("\n")); err == nil {
		t.Error("empty password succeeded")
	}
	if _, err := ReadPassword(strings.NewReader(strings.Repeat("a", 5000))); err == nil {
		t.Error("oversized password succeeded")
	}
}

func TestGeneratePassword(t *testing.T) {
	first, err := GeneratePassword()
	if err != nil || len(first) != 24 {
		t.Fatalf("GeneratePassword = %q, %v", first, err)
	}
	if second, _ := GeneratePassword(); second == first {
		t.Error("two passwords are equal")
	}
}

func TestDialAddress(t *testing.T) {
	cases := map[string]string{
		"localhost:8080": "localhost:8080",
		"0.0.0.0:8080":   "127.0.0.1:8080",
		":8080":          "127.0.0.1:8080",
		"[::]:8080":      "127.0.0.1:8080",
		"10.0.0.2:9":     "10.0.0.2:9",
	}
	for listen, want := range cases {
		if got := DialAddress(listen); got != want {
			t.Errorf("DialAddress(%q) = %q, want %q", listen, got, want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{
		45 * time.Second:                "45s",
		5*time.Minute + 10*time.Second:  "5m10s",
		2*time.Hour + 13*time.Minute:    "2h13m",
		3*24*time.Hour + 4*time.Hour:    "3d4h",
		-time.Minute:                    "0s",
		90*time.Minute + 29*time.Second: "1h30m",
	}
	for duration, want := range cases {
		if got := FormatDuration(duration); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", duration, got, want)
		}
	}
}

func TestTailLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := TailLines(path, 2); err != nil || got != "c\nd" {
		t.Errorf("TailLines = %q, %v", got, err)
	}
	if got, err := TailLines(path, 10); err != nil || got != "a\nb\nc\nd" {
		t.Errorf("TailLines all = %q, %v", got, err)
	}
	if _, err := TailLines(filepath.Join(t.TempDir(), "absent"), 2); err == nil {
		t.Error("missing file succeeded")
	}
}
