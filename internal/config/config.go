// Package config holds the runtime configuration populated from CLI flags.
package config

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// Port is a TCP port number (1-65535). Implements pflag.Value.
type Port uint16

func (p *Port) String() string { return strconv.Itoa(int(*p)) }
func (p *Port) Type() string   { return "Port" }
func (p *Port) Set(s string) error {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fmt.Errorf("invalid port %q", s)
	}
	if n < 1 || n > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	*p = Port(n)
	return nil
}

// LogLevel controls slog verbosity. Implements pflag.Value.
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

func (l *LogLevel) String() string { return string(*l) }
func (l *LogLevel) Type() string   { return "LogLevel" }
func (l *LogLevel) Set(s string) error {
	n := strings.ToLower(strings.TrimSpace(s))
	switch n {
	case "debug", "info", "warn", "warning", "error":
		if n == "warning" {
			n = "warn"
		}
		*l = LogLevel(n)
		return nil
	default:
		return fmt.Errorf("must be one of debug, info, warn, error")
	}
}

// SlogLevel returns the slog.Level for this LogLevel.
func (l LogLevel) SlogLevel() slog.Level {
	switch l {
	case LogLevelDebug:
		return slog.LevelDebug
	case LogLevelWarn:
		return slog.LevelWarn
	case LogLevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Config is the central runtime configuration for llm-router.
// All values originate from CLI flags — there is no config file.
type Config struct {
	// DashboardAddr is the address the dashboard HTTP server binds to (e.g. "localhost:8080").
	DashboardAddr string

	// APIAddr is the address the /v1 OpenAI-compatible API binds to (e.g. "localhost:8081").
	APIAddr string

	// DBPath is the path to the bbolt database file.
	DBPath string

	// LogLevel controls slog verbosity.
	LogLevel LogLevel

	// TestingKeyPath is the path to the file holding the ephemeral testing bearer token.
	// Empty means the feature is disabled.
	TestingKeyPath string

	// TestingKey is the raw testing token value (never persisted to DB, never logged).
	TestingKey string

	// DevUIRedirect, when non-empty, is the origin (e.g. "http://localhost:8080")
	// that the dashboard 302-redirects browser navigations to instead of
	// serving its own embedded SPA. Set only by `make start` (via
	// --dev-ui-redirect) in dev; empty disables the redirect — the normal/production behavior.
	DevUIRedirect string
}

// Validate checks that all required configuration fields are set.
func (c *Config) Validate() error {
	if c.DashboardAddr == "" {
		return fmt.Errorf("dashboard listen address is required")
	}
	if c.APIAddr == "" {
		return fmt.Errorf("api listen address is required")
	}
	if c.DBPath == "" {
		return fmt.Errorf("database path is required")
	}
	switch c.LogLevel {
	case "", LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
	default:
		if string(c.LogLevel) == "warning" {
			break
		}
		return fmt.Errorf("log-level must be one of debug, info, warn, error")
	}
	return nil
}
