package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	bolt "go.etcd.io/bbolt"
)

// persistBucket saves a bucket to the database.
func (s *Service) persistBucket(bucket *MetricBucket, granularity string) error {
	if bucket == nil {
		return nil
	}

	if err := s.database.Update(func(tx *bolt.Tx) error {
		metricsBucket := tx.Bucket(db.BucketMetrics)
		if metricsBucket == nil {
			return fmt.Errorf("metrics bucket not found")
		}

		key := bucketKey(bucket.Timestamp, granularity)
		data, err := json.Marshal(bucket)
		if err != nil {
			return fmt.Errorf("marshal bucket: %w", err)
		}

		return metricsBucket.Put([]byte(key), data)
	}); err != nil {
		return err
	}

	if err := s.persistBucketSQLite(context.Background(), bucket, granularity); err != nil {
		return fmt.Errorf("persist bucket sqlite: %w", err)
	}
	return nil
}

// loadBucket loads a single bucket from the database.
func (s *Service) loadBucket(timestamp time.Time, granularity string) (*MetricBucket, error) {
	if s.bucketRepository != nil {
		bucket, err := s.loadBucketSQLite(context.Background(), timestamp, granularity)
		if err == nil {
			return bucket, nil
		}
		if !errors.Is(err, apierrors.ErrNotFound) {
			return nil, err
		}
	}

	var bucket MetricBucket

	err := s.database.View(func(tx *bolt.Tx) error {
		metricsBucket := tx.Bucket(db.BucketMetrics)
		if metricsBucket == nil {
			return fmt.Errorf("metrics bucket not found")
		}

		key := bucketKey(timestamp, granularity)
		data := metricsBucket.Get([]byte(key))
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

	err := s.database.View(func(tx *bolt.Tx) error {
		metricsBucket := tx.Bucket(db.BucketMetrics)
		if metricsBucket == nil {
			return nil
		}

		cursor := metricsBucket.Cursor()
		for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
			timestamp, _, err := parseBucketKey(string(key))
			if err != nil {
				continue
			}

			// Check if timestamp is within range
			if (timestamp.Equal(start) || timestamp.After(start)) && (timestamp.Equal(end) || timestamp.Before(end)) {
				var bucket MetricBucket
				if err := json.Unmarshal(value, &bucket); err != nil {
					s.logger.Warn("failed to unmarshal bucket", "key", string(key), "err", err)
					continue
				}
				buckets = append(buckets, &bucket)
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	sqliteBuckets, err := s.loadBucketsInRangeSQLite(context.Background(), start, end)
	if err != nil {
		return nil, err
	}

	return mergeSQLiteBuckets(buckets, sqliteBuckets), nil
}

// deleteBucketsOlderThan deletes all buckets older than the cutoff time.
func (s *Service) deleteBucketsOlderThan(cutoff time.Time) error {
	var toDelete []string

	// First, collect keys to delete
	err := s.database.View(func(tx *bolt.Tx) error {
		metricsBucket := tx.Bucket(db.BucketMetrics)
		if metricsBucket == nil {
			return nil
		}

		cursor := metricsBucket.Cursor()
		for key, _ := cursor.First(); key != nil; key, _ = cursor.Next() {
			timestamp, _, err := parseBucketKey(string(key))
			if err != nil {
				continue
			}

			if timestamp.Before(cutoff) {
				toDelete = append(toDelete, string(key))
			}
		}
		return nil
	})

	if err != nil {
		return err
	}

	if len(toDelete) == 0 {
		_, sqliteErr := s.deleteBucketsOlderThanSQLite(context.Background(), cutoff)
		return sqliteErr
	}

	// Delete collected keys
	err = s.database.Update(func(tx *bolt.Tx) error {
		metricsBucket := tx.Bucket(db.BucketMetrics)
		if metricsBucket == nil {
			return nil
		}

		for _, key := range toDelete {
			if err := metricsBucket.Delete([]byte(key)); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return err
	}

	s.logger.Info("deleted old buckets", "count", len(toDelete))

	if _, sqliteErr := s.deleteBucketsOlderThanSQLite(context.Background(), cutoff); sqliteErr != nil {
		return sqliteErr
	}

	return nil
}

// loadTokenUsageFromDB loads all-time token usage from database.
// Token breakdowns live in the token dimension child table, so the SQLite
// base rows contribute nothing here.
// TODO(Wave 7/8): aggregate metric_bucket_tokens once the token dimension lands.
func (s *Service) loadTokenUsageFromDB() (map[string]*TokenUsageInfo, error) {
	usage := make(map[string]*TokenUsageInfo)

	err := s.database.View(func(tx *bolt.Tx) error {
		metricsBucket := tx.Bucket(db.BucketMetrics)
		if metricsBucket == nil {
			return nil
		}

		cursor := metricsBucket.Cursor()
		for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
			var bucket MetricBucket
			if err := json.Unmarshal(value, &bucket); err != nil {
				s.logger.Warn("failed to unmarshal bucket", "key", string(key), "err", err)
				continue
			}

			for tokenID, tokenMetrics := range bucket.ByTokenID {
				if usage[tokenID] == nil {
					usage[tokenID] = &TokenUsageInfo{}
				}
				usage[tokenID].Requests += tokenMetrics.Requests

				// Keep most recent LastUsed
				if tokenMetrics.LastUsed != nil {
					if usage[tokenID].LastUsed == nil || tokenMetrics.LastUsed.After(*usage[tokenID].LastUsed) {
						usage[tokenID].LastUsed = tokenMetrics.LastUsed
					}
				}
			}
		}
		return nil
	})

	return usage, err
}
