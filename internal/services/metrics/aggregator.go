package metrics

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
	"github.com/TheSlopMachine/llm-router/internal/db"
)

// compressAndPersist compresses old in-memory buckets and persists them to bbolt.
func (s *Service) compressAndPersist() error {
	now := time.Now()
	cutoff := now.Add(-1 * time.Hour)

	s.mu.Lock()
	var toCompress []*MetricBucket
	var toDelete []time.Time

	for ts, bucket := range s.recentBuckets {
		if ts.Before(cutoff) {
			toCompress = append(toCompress, bucket)
			toDelete = append(toDelete, ts)
		}
	}

	// Remove from memory
	for _, ts := range toDelete {
		delete(s.recentBuckets, ts)
	}
	s.mu.Unlock()

	if len(toCompress) == 0 {
		return nil
	}

	// Group into 30-minute windows and compress
	grouped := s.groupBuckets(toCompress, 30*time.Minute)
	for _, buckets := range grouped {
		compressed := s.compressBuckets(buckets, 30*time.Minute)
		if err := s.persistBucket(compressed, "30m"); err != nil {
			return fmt.Errorf("persist 30m bucket: %w", err)
		}
	}

	s.logger.Info("compressed and persisted buckets", "count", len(toCompress), "windows", len(grouped))
	return nil
}

// groupBuckets groups buckets into time windows.
func (s *Service) groupBuckets(buckets []*MetricBucket, window time.Duration) map[time.Time][]*MetricBucket {
	grouped := make(map[time.Time][]*MetricBucket)
	for _, bucket := range buckets {
		windowStart := bucket.Timestamp.Truncate(window)
		grouped[windowStart] = append(grouped[windowStart], bucket)
	}
	return grouped
}

// compressBuckets aggregates multiple buckets into one.
func (s *Service) compressBuckets(buckets []*MetricBucket, targetInterval time.Duration) *MetricBucket {
	if len(buckets) == 0 {
		return nil
	}

	// Use the first bucket's truncated timestamp as the base
	compressed := &MetricBucket{
		Timestamp:      buckets[0].Timestamp.Truncate(targetInterval),
		ByProviderID:   make(map[string]*ProviderMetrics),
		ByProviderType: make(map[string]*ProviderMetrics),
		ByModel:        make(map[string]*ModelMetrics),
		ByTokenID:      make(map[string]*TokenMetrics),
		ErrorsByType:   make(map[string]int64),
	}

	for _, bucket := range buckets {
		// Aggregate totals
		compressed.TotalRequests += bucket.TotalRequests
		compressed.TotalErrors += bucket.TotalErrors
		compressed.TokensInput += bucket.TokensInput
		compressed.TokensOutput += bucket.TokensOutput
		compressed.DurationSum += bucket.DurationSum
		compressed.DurationCount += bucket.DurationCount

		// Aggregate provider ID metrics
		for id, pm := range bucket.ByProviderID {
			if compressed.ByProviderID[id] == nil {
				compressed.ByProviderID[id] = &ProviderMetrics{}
			}
			s.mergeProviderMetrics(compressed.ByProviderID[id], pm)
		}

		// Aggregate provider type metrics
		for typ, pm := range bucket.ByProviderType {
			if compressed.ByProviderType[typ] == nil {
				compressed.ByProviderType[typ] = &ProviderMetrics{}
			}
			s.mergeProviderMetrics(compressed.ByProviderType[typ], pm)
		}

		// Aggregate model metrics
		for model, mm := range bucket.ByModel {
			if compressed.ByModel[model] == nil {
				compressed.ByModel[model] = &ModelMetrics{}
			}
			s.mergeModelMetrics(compressed.ByModel[model], mm)
		}

		// Aggregate token metrics
		for tokenID, tm := range bucket.ByTokenID {
			if compressed.ByTokenID[tokenID] == nil {
				compressed.ByTokenID[tokenID] = &TokenMetrics{}
			}
			s.mergeTokenMetrics(compressed.ByTokenID[tokenID], tm)
		}

		// Aggregate error types
		for errType, count := range bucket.ErrorsByType {
			compressed.ErrorsByType[errType] += count
		}
	}

	return compressed
}

// mergeProviderMetrics merges source into destination.
func (s *Service) mergeProviderMetrics(dest, src *ProviderMetrics) {
	dest.Requests += src.Requests
	dest.Errors += src.Errors
	dest.TokensInput += src.TokensInput
	dest.TokensOutput += src.TokensOutput
	dest.DurationSum += src.DurationSum
	dest.DurationCount += src.DurationCount
}

// mergeModelMetrics merges source into destination.
func (s *Service) mergeModelMetrics(dest, src *ModelMetrics) {
	dest.Requests += src.Requests
	dest.Errors += src.Errors
	dest.TokensInput += src.TokensInput
	dest.TokensOutput += src.TokensOutput
}

// mergeTokenMetrics merges source into destination.
func (s *Service) mergeTokenMetrics(dest, src *TokenMetrics) {
	dest.Requests += src.Requests
	dest.Errors += src.Errors
	dest.TokensInput += src.TokensInput
	dest.TokensOutput += src.TokensOutput
	
	// Keep the most recent LastUsed timestamp
	if src.LastUsed != nil {
		if dest.LastUsed == nil || src.LastUsed.After(*dest.LastUsed) {
			dest.LastUsed = src.LastUsed
		}
	}
}

// compressOldBuckets compresses buckets of one granularity into another.
func (s *Service) compressOldBuckets(cutoff time.Time, fromGranularity, toGranularity string, targetInterval time.Duration) error {
	// Load buckets from database
	buckets, err := s.loadBucketsByGranularity(cutoff, fromGranularity)
	if err != nil {
		return fmt.Errorf("load buckets: %w", err)
	}

	if len(buckets) == 0 {
		return nil
	}

	// Group and compress
	grouped := s.groupBuckets(buckets, targetInterval)
	for _, group := range grouped {
		compressed := s.compressBuckets(group, targetInterval)
		if err := s.persistBucket(compressed, toGranularity); err != nil {
			return fmt.Errorf("persist %s bucket: %w", toGranularity, err)
		}
	}

	// Delete old buckets
	for _, bucket := range buckets {
		key := bucketKey(bucket.Timestamp, fromGranularity)
		if err := s.deleteBucket(key); err != nil {
			return fmt.Errorf("delete old bucket: %w", err)
		}
	}

	s.logger.Info("compressed old buckets", "from", fromGranularity, "to", toGranularity, "count", len(buckets))
	return nil
}

// loadBucketsByGranularity loads all buckets with a specific granularity older than cutoff.
func (s *Service) loadBucketsByGranularity(cutoff time.Time, granularity string) ([]*MetricBucket, error) {
	var buckets []*MetricBucket

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketMetrics)
		if b == nil {
			return nil
		}

		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			ts, gran, err := parseBucketKey(string(k))
			if err != nil {
				continue
			}

			if gran == granularity && ts.Before(cutoff) {
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

// deleteBucket deletes a bucket by key.
func (s *Service) deleteBucket(key string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketMetrics)
		if b == nil {
			return nil
		}
		return b.Delete([]byte(key))
	})
}

// bucketKey generates a storage key for a bucket.
// Format: "2026-05-04T19:00:00_1m"
func bucketKey(t time.Time, granularity string) string {
	return t.UTC().Format(time.RFC3339) + "_" + granularity
}

// parseBucketKey parses a bucket key back into timestamp and granularity.
func parseBucketKey(key string) (time.Time, string, error) {
	// Find the last underscore
	idx := -1
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '_' {
			idx = i
			break
		}
	}

	if idx == -1 {
		return time.Time{}, "", fmt.Errorf("invalid bucket key format: %s", key)
	}

	tsStr := key[:idx]
	granularity := key[idx+1:]

	ts, err := time.Parse(time.RFC3339, tsStr)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("parse timestamp: %w", err)
	}

	return ts, granularity, nil
}

