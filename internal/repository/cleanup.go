package repository

import (
	"encoding/json"
	"time"

	bolt "go.etcd.io/bbolt"
	"github.com/TheSlopMachine/llm-router/internal/db"
)

// CleanupExpired removes records from a bucket that are older than the given threshold.
// The getTimestamp function extracts the timestamp to compare from each record.
//
// This is a two-phase operation:
//  1. View transaction: collect keys of expired records
//  2. Update transaction: delete collected keys
//
// Example:
//
//	err := repository.CleanupExpired(db, db.BucketAuth, threshold, func(e *Entry) time.Time {
//	    return e.CreatedAt
//	})
func CleanupExpired[T any](
	database *db.DB,
	bucket []byte,
	threshold time.Time,
	getTimestamp func(*T) time.Time,
) error {
	var toDelete [][]byte

	// Phase 1: Collect keys to delete
	err := database.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var item T
			if err := json.Unmarshal(v, &item); err != nil {
				// Skip malformed records
				return nil
			}
			if getTimestamp(&item).Before(threshold) {
				// Make a copy of the key since it's only valid during the transaction
				keyCopy := make([]byte, len(k))
				copy(keyCopy, k)
				toDelete = append(toDelete, keyCopy)
			}
			return nil
		})
	})
	if err != nil {
		return err
	}

	if len(toDelete) == 0 {
		return nil
	}

	// Phase 2: Delete collected keys
	return database.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		for _, k := range toDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

