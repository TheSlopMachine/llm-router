// Package cli holds the pure helpers of the command line: argument parsing
// and text formatting with no dependency on the server.
package cli

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// ParseSeverity accepts a level name (debug, info, warn, error, fatal) or
// its number 0-4.
func ParseSeverity(raw string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug", "0":
		return 0, nil
	case "info", "1":
		return 1, nil
	case "warn", "warning", "2":
		return 2, nil
	case "error", "3":
		return 3, nil
	case "fatal", "4":
		return 4, nil
	}
	return 0, fmt.Errorf("invalid severity %q: want debug, info, warn, error or fatal", raw)
}

// ParseTimeBound accepts an RFC 3339 timestamp or an age such as 30m, 6h
// or 2d, which resolves to now minus that age.
func ParseTimeBound(raw string, now time.Time) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return parsed, nil
	}
	if age, err := parseAge(trimmed); err == nil {
		return now.Add(-age), nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q: want RFC 3339 (2026-10-21T09:00:00Z) or an age like 30m, 6h, 2d", raw)
}

func parseAge(raw string) (time.Duration, error) {
	if days, found := strings.CutSuffix(raw, "d"); found {
		count, err := strconv.Atoi(days)
		if err != nil || count < 0 {
			return 0, errors.New("invalid day count")
		}
		return time.Duration(count) * 24 * time.Hour, nil
	}
	age, err := time.ParseDuration(raw)
	if err != nil || age < 0 {
		return 0, errors.New("invalid duration")
	}
	return age, nil
}

// ParseKeyFilter splits "key" or "key=value". A nil value matches key
// presence only.
func ParseKeyFilter(raw string) (string, *string, error) {
	key, value, found := strings.Cut(raw, "=")
	key = strings.TrimSpace(key)
	if key == "" {
		return "", nil, fmt.Errorf("invalid filter %q: want key or key=value", raw)
	}
	if !found {
		return key, nil, nil
	}
	value = strings.TrimSpace(value)
	return key, &value, nil
}

// ReadPassword reads one password from reader and strips the line ending.
// An empty result fails.
func ReadPassword(reader io.Reader) (string, error) {
	const limit = 4096
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if len(data) > limit {
		return "", fmt.Errorf("password is longer than %d bytes", limit)
	}
	password := strings.TrimRight(string(data), "\r\n")
	if password == "" {
		return "", errors.New("password must not be empty")
	}
	return password, nil
}

// GeneratePassword returns 24 random URL-safe characters.
func GeneratePassword() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// DialAddress turns a listen address into one a local client can connect
// to: wildcard hosts become the loopback address.
func DialAddress(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// FormatDuration renders an uptime in the two largest units: 3d4h, 2h13m,
// 5m10s, 45s.
func FormatDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	duration = duration.Round(time.Second)
	days := int(duration / (24 * time.Hour))
	hours := int(duration/time.Hour) % 24
	minutes := int(duration/time.Minute) % 60
	seconds := int(duration/time.Second) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

// TailLines returns the last count lines of the file at path.
func TailLines(path string, count int) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return strings.Join(lines, "\n"), nil
}
