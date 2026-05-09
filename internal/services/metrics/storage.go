package metrics

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
	"github.com/TheSlopMachine/llm-router/internal/db"
)

// persistBucket saves a bucket to the database.
func (s *Service) persistBucket(bucket *MetricBucket, granularity string) error {
	if bucket == nil {
		return nil
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketMetrics)
		if b == nil {
			return fmt.Errorf("metrics bucket not found")
		}

		key := bucketKey(bucket.Timestamp, granularity)
		data, err := json.Marshal(bucket)
		if err != nil {
			return fmt.Errorf("marshal bucket: %w", err)
		}

		return b.Put([]byte(key), data)
	})
}

// loadBucket loads a single bucket from the database.
func (s *Service) loadBucket(timestamp time.Time, granularity string) (*MetricBucket, error) {
	var bucket MetricBucket

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketMetrics)
		if b == nil {
			return fmt.Errorf("metrics bucket not found")
		}

		key := bucketKey(timestamp, granularity)
		data := b.Get([]byte(key))
		if data == nil {
			return fmt.Errorf("bucket not found")
		}

		return json.Unmarshal(data, &bucket)
	})

	if err != nil {
		return nil, err
	}

	return &bucket, nil
}

// loadBucketsInRange loads all buckets within a time range from the database.
func (s *Service) loadBucketsInRange(start, end time.Time) ([]*MetricBucket, error) {
	var buckets []*MetricBucket

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketMetrics)
		if b == nil {
			return nil
		}

		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			ts, _, err := parseBucketKey(string(k))
			if err != nil {
				continue
			}

			// Check if timestamp is within range
			if (ts.Equal(start) || ts.After(start)) && (ts.Equal(end) || ts.Before(end)) {
				var bucket MetricBucket
				if err := json.Unmarshal(v, &bucket); err != nil {
					s.logger.Warn("failed to unmarshal bucket", "key", string(k), "err", err)
					continue
				}
				buckets = append(buckets, &bucket)
			}
		}
		return nil
	})

	return buckets, err
}

// deleteBucketsOlderThan deletes all buckets older than the cutoff time.
func (s *Service) deleteBucketsOlderThan(cutoff time.Time) error {
	var toDelete []string

	// First, collect keys to delete
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketMetrics)
		if b == nil {
			return nil
		}

		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			ts, _, err := parseBucketKey(string(k))
			if err != nil {
				continue
			}

			if ts.Before(cutoff) {
				toDelete = append(toDelete, string(k))
			}
		}
		return nil
	})

	if err != nil {
		return err
	}

	if len(toDelete) == 0 {
		return nil
	}

	// Delete collected keys
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketMetrics)
		if b == nil {
			return nil
		}

		for _, key := range toDelete {
			if err := b.Delete([]byte(key)); err != nil {
				return err
			}
		}
		return nil
	})

	if err == nil {
		s.logger.Info("deleted old buckets", "count", len(toDelete))
	}

	return err
}

// loadTokenUsageFromDB loads all-time token usage from database.
func (s *Service) loadTokenUsageFromDB() (map[string]*TokenUsageInfo, error) {
	usage := make(map[string]*TokenUsageInfo)

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketMetrics)
		if b == nil {
			return nil
		}

		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var bucket MetricBucket
			if err := json.Unmarshal(v, &bucket); err != nil {
				s.logger.Warn("failed to unmarshal bucket", "key", string(k), "err", err)
				continue
			}

			for tokenID, tm := range bucket.ByTokenID {
				if usage[tokenID] == nil {
					usage[tokenID] = &TokenUsageInfo{}
				}
				usage[tokenID].Requests += tm.Requests

				// Keep most recent LastUsed
				if tm.LastUsed != nil {
					if usage[tokenID].LastUsed == nil || tm.LastUsed.After(*usage[tokenID].LastUsed) {
						usage[tokenID].LastUsed = tm.LastUsed
					}
				}
			}
		}
		return nil
	})

	return usage, err
}

