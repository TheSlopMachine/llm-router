// Package config holds the runtime configuration populated from CLI flags.
package config

import "fmt"

// Config is the central runtime configuration for llm-router.
// All values originate from CLI flags — there is no config file.
type Config struct {
	// ListenAddr is the address the HTTP server binds to (e.g. "localhost:8080").
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
	if c.ListenAddr == "" {
		return fmt.Errorf("listen address is required")
	}
	if c.DBPath == "" {
		return fmt.Errorf("database path is required")
	}
	return nil
}

