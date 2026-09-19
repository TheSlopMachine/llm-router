// Package db manages the embedded bbolt database and all bucket definitions.
package db

import (
	"errors"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

// Bucket names — the single source of truth for all DB keys.
var (
	BucketMeta                = []byte("meta")                 // bootstrap state, schema version
	BucketAdmin               = []byte("admin")                // AdminUser records
	BucketTokens              = []byte("tokens")               // RouterToken records (keyed by ID)
	BucketTokenIndex          = []byte("token_index")          // token value → token ID lookup
	BucketProviders           = []byte("providers")            // Legacy bucket, no longer used as provider source of truth
	BucketProviderInstances   = []byte("provider_instances")   // Unified provider records (all types)
	BucketCustomProviders     = []byte("custom_providers")     // Legacy custom providers, migrated to provider_instances on startup
	BucketCredentials         = []byte("credentials")          // Credential records
	BucketPlugins             = []byte("plugins")              // Installed Lua plugin records
	BucketPluginRepos         = []byte("plugin_repos")         // Added plugin repositories
	BucketPluginStorage       = []byte("plugin_storage")       // llm_router.storage.* key/value per plugin
	BucketAuth                = []byte("auth")                 // Ephemeral auth state
	BucketModelInfo           = []byte("model_info")           // Legacy bucket, no longer used for model metadata caching
	BucketSessions            = []byte("sessions")             // Dashboard sessions
	BucketMetrics             = []byte("metrics")              // Time-series metrics data
	BucketVirtualModels       = []byte("virtual_models")       // VirtualModel records
	BucketRouterConfiguration = []byte("router_configuration") // Instance configuration (RouterConfiguration)
	BucketModelOverrides      = []byte("model_overrides")      // Per-provider model enable/disable and custom models
	BucketModelInfos          = []byte("model_infos")          // Persisted per-provider model metadata cache
	BucketProxies             = []byte("proxies_v2")           // Proxy pool records (manual + list-sourced)
	BucketProxyLimits         = []byte("proxy_limits")         // Per-pair live state: (proxy, provider) limits and blocks
	BucketActiveRegions       = []byte("active_regions")       // Demanded proxy exit locations
	BucketProxySourceMeta     = []byte("proxy_source_meta")    // Last fetch totals per proxy list source
)

// DB wraps a bbolt.DB and ensures all required buckets exist.
type DB struct {
	*bolt.DB
}

// Open opens (or creates) the bbolt database at the given path and
// ensures every required bucket is created.
func Open(path string) (*DB, error) {
	bdb, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("open bbolt db at %q: %w", path, err)
	}

	db := &DB{bdb}
	if err := db.initBuckets(); err != nil {
		bdb.Close()
		return nil, err
	}
	return db, nil
}

// initBuckets creates all top-level buckets if they do not yet exist.
func (db *DB) initBuckets() error {
	return db.Update(func(tx *bolt.Tx) error {
		buckets := [][]byte{
			BucketMeta,
			BucketAdmin,
			BucketTokens,
			BucketTokenIndex,
			BucketProviderInstances,
			BucketCustomProviders,
			BucketCredentials,
			BucketPlugins,
			BucketPluginRepos,
			BucketPluginStorage,
			BucketAuth,
			BucketSessions,
			BucketMetrics,
			BucketVirtualModels,
			BucketRouterConfiguration,
			BucketModelOverrides,
			BucketModelInfos,
			BucketProxies,
			BucketProxyLimits,
			BucketActiveRegions,
			BucketProxySourceMeta,
		}
		for _, name := range buckets {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return fmt.Errorf("create bucket %q: %w", name, err)
			}
		}
		// The pre-rework "agents" bucket is dead weight; virtual models live in
		// virtual_models now.
		if err := tx.DeleteBucket([]byte("agents")); err != nil && !errors.Is(err, bolt.ErrBucketNotFound) {
			return fmt.Errorf("drop legacy agents bucket: %w", err)
		}
		return nil
	})
}

// IsBootstrapped returns true once the admin account has been created.
func (db *DB) IsBootstrapped() (bool, error) {
	var bootstrapped bool
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(BucketMeta)
		v := b.Get([]byte("bootstrapped"))
		bootstrapped = string(v) == "true"
		return nil
	})
	return bootstrapped, err
}

// SetBootstrapped marks the database as fully initialized.
func (db *DB) SetBootstrapped() error {
	return db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(BucketMeta).Put([]byte("bootstrapped"), []byte("true"))
	})
}
