// Package config holds the runtime configuration populated from CLI flags.
package config

import "fmt"

// Config is the central runtime configuration for llm-router.
// All values originate from CLI flags — there is no config file.
type Config struct {
	// DashboardAddr is the address the dashboard HTTP server binds to (e.g. "localhost:8080").
	DashboardAddr string

	// APIAddr is the address the /v1 OpenAI-compatible API binds to (e.g. "localhost:8081").
	APIAddr string

	// DBPath is the path to the bbolt database file.
	DBPath string

	// LogLevel controls slog verbosity: debug, info, warn, error.
	LogLevel string

	// MaxCredentialRetries is the number of retry cycles for credential rotation.
	// Default: 7 (exponential backoff: 1s→2s→4s→8s→16s→32s→64s)
	MaxCredentialRetries int

	// TestingKeyPath is the path to the file holding the ephemeral testing bearer token.
	// Empty means the feature is disabled.
	TestingKeyPath string

	// TestingKey is the raw testing token value (never persisted to DB, never logged).
	TestingKey string
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
	case "", "debug", "info", "warn", "warning", "error":
	default:
		return fmt.Errorf("log-level must be one of debug, info, warn, error")
	}
	return nil
}
