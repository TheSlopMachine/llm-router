// Package db manages the embedded bbolt database and all bucket definitions.
package db

import (
	"fmt"

	bolt "go.etcd.io/bbolt"
)

// Bucket names — the single source of truth for all DB keys.
var (
	BucketMeta        = []byte("meta")        // bootstrap state, schema version
	BucketAdmin       = []byte("admin")       // AdminUser records
	BucketTokens      = []byte("tokens")      // RouterToken records (keyed by ID)
	BucketTokenIndex  = []byte("token_index") // token value → token ID lookup
	BucketProviders   = []byte("providers")   // Legacy bucket, no longer used as provider source of truth
	BucketCredentials = []byte("credentials") // Credential records
	BucketAuth        = []byte("auth")        // Ephemeral auth state
	BucketModelInfo   = []byte("model_info")  // Legacy bucket, no longer used for model metadata caching
	BucketSessions    = []byte("sessions")    // Dashboard sessions
	BucketMetrics     = []byte("metrics")     // Time-series metrics data
	BucketAgents      = []byte("agents")      // Agent records
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
			BucketCredentials,
			BucketAuth,
			BucketSessions,
			BucketMetrics,
			BucketAgents,
		}
		for _, name := range buckets {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return fmt.Errorf("create bucket %q: %w", name, err)
			}
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

