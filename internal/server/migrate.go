package server

import (
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/TheSlopMachine/llm-router/internal/db"
	bolt "go.etcd.io/bbolt"
)

// legacyProxyLimitsBucket held per-pair proxy rate limits and blocks before
// 0.1.1 replaced them with the unified exhausted store.
var legacyProxyLimitsBucket = []byte("proxy_limits")

// migrateDropProxyLimits deletes the pre-0.1.1 proxy limits bucket.
// Idempotent: a missing bucket reports nothing to do.
func migrateDropProxyLimits(logger *slog.Logger, database *db.DB) {
	err := database.Update(func(tx *bolt.Tx) error {
		return tx.DeleteBucket(legacyProxyLimitsBucket)
	})
	if err == nil {
		logger.Info("migration completed: dropped legacy proxy_limits bucket")
		return
	}
	if !errors.Is(err, bolt.ErrBucketNotFound) {
		logger.Warn("migration failed: drop proxy_limits bucket", "err", err)
	}
}

// migrateClearCredentialQuota strips the pre-0.1.1 quota_reset_at field from
// stored credential rows. The field no longer exists on the struct, so plain
// reads would keep dead bytes forever; the rewrite drops them once.
// Idempotent: reruns find no rows carrying the field.
func migrateClearCredentialQuota(logger *slog.Logger, database *db.DB) {
	cleared := 0
	err := database.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(db.BucketCredentials)
		if bkt == nil {
			return nil
		}
		var rewrite []string
		var payloads [][]byte
		if err := bkt.ForEach(func(k, v []byte) error {
			var row map[string]any
			if err := json.Unmarshal(v, &row); err != nil {
				return nil
			}
			if _, ok := row["quota_reset_at"]; !ok {
				return nil
			}
			delete(row, "quota_reset_at")
			enc, err := json.Marshal(row)
			if err != nil {
				return nil
			}
			rewrite = append(rewrite, string(k))
			payloads = append(payloads, enc)
			return nil
		}); err != nil {
			return err
		}
		for i, key := range rewrite {
			if err := bkt.Put([]byte(key), payloads[i]); err != nil {
				return err
			}
			cleared++
		}
		return nil
	})
	if err != nil {
		logger.Warn("migration failed: clear credential quota fields", "err", err)
		return
	}
	if cleared > 0 {
		logger.Info("migration completed: cleared credential quota fields", "count", cleared)
	}
}
