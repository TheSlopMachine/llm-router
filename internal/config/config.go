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

	// Deprecated: ListenAddr kept for backwards compat, use DashboardAddr/APIAddr.
	ListenAddr string

	// DBPath is the path to the bbolt database file.
	DBPath string

	// Debug enables verbose request/response logging.
	Debug bool

	// MaxCredentialRetries is the number of retry cycles for credential rotation.
	// Default: 7 (exponential backoff: 1s→2s→4s→8s→16s→32s→64s)
	MaxCredentialRetries int
}

// Validate checks that all required configuration fields are set.
func (c *Config) Validate() error {
	if c.DashboardAddr == "" && c.ListenAddr == "" {
		return fmt.Errorf("dashboard listen address is required")
	}
	if c.APIAddr == "" && c.ListenAddr == "" {
		return fmt.Errorf("api listen address is required")
	}
	if c.DBPath == "" {
		return fmt.Errorf("database path is required")
	}
	return nil
}

