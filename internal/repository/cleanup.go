package repository

import (
	"encoding/json"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	bolt "go.etcd.io/bbolt"
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
	bucketName []byte,
	threshold time.Time,
	getTimestamp func(*T) time.Time,
) error {
	var toDelete [][]byte

	// Phase 1: Collect keys to delete
	err := database.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketName)
		if bucket == nil {
			return nil
		}
		return bucket.ForEach(func(key, value []byte) error {
			var item T
			if err := json.Unmarshal(value, &item); err != nil {
				// Skip malformed records
				return nil
			}
			if getTimestamp(&item).Before(threshold) {
				// Make a copy of the key since it's only valid during the transaction
				keyCopy := make([]byte, len(key))
				copy(keyCopy, key)
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
		bucket := tx.Bucket(bucketName)
		for _, key := range toDelete {
			if err := bucket.Delete(key); err != nil {
				return err
			}
		}
		return nil
	})
}
