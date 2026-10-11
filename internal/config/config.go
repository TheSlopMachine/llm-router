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
func (p *Port) Set(value string) error {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("invalid port %q", value)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	*p = Port(port)
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
func (l *LogLevel) Set(value string) error {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "debug", "info", "warn", "warning", "error":
		if normalized == "warning" {
			normalized = "warn"
		}
		*l = LogLevel(normalized)
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

	// NoAuth disables all authorization: /v1 requests validate without
	// bearer tokens, dashboard APIs skip session checks, and status
	// reports authenticated. Bootstrap state stays real: a fresh DB
	// still opens the account-creation page. Dev and AI debugging only —
	// never enable on shared or public instances.
	NoAuth bool

	// DevUIRedirect, when non-empty, is the origin (e.g. "http://localhost:8080")
	// that the dashboard 302-redirects browser navigations to instead of
	// serving its own embedded SPA. Set only by `make start` (via
	// --dev-ui-redirect) in dev; empty disables the redirect — the normal/production behavior.
	DevUIRedirect string
}

// Validate checks that all required configuration fields are set.
func (configuration *Config) Validate() error {
	if configuration.DashboardAddr == "" {
		return fmt.Errorf("dashboard listen address is required")
	}
	if configuration.APIAddr == "" {
		return fmt.Errorf("api listen address is required")
	}
	if configuration.DBPath == "" {
		return fmt.Errorf("database path is required")
	}
	switch configuration.LogLevel {
	case "", LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
	default:
		if string(configuration.LogLevel) == "warning" {
			break
		}
		return fmt.Errorf("log-level must be one of debug, info, warn, error")
	}
	return nil
}
