package metrics

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	bolt "go.etcd.io/bbolt"
)

// compressAndPersist compresses old in-memory buckets and persists them to bbolt.
func (s *Service) compressAndPersist() error {
	now := time.Now()
	cutoff := now.Add(-1 * time.Hour)

	s.mutex.Lock()
	var toCompress []*MetricBucket
	var toDelete []time.Time

	for timestamp, bucket := range s.recentBuckets {
		if timestamp.Before(cutoff) {
			toCompress = append(toCompress, bucket)
			toDelete = append(toDelete, timestamp)
		}
	}
	s.mutex.Unlock()

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

	// Evict from memory only after durable writes.
	s.mutex.Lock()
	for _, timestamp := range toDelete {
		delete(s.recentBuckets, timestamp)
	}
	s.mutex.Unlock()

	s.logger.Debug("compressed and persisted buckets", "count", len(toCompress), "windows", len(grouped))
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
		// Aggregate totals (sum), peaks (max per-minute)
		compressed.TotalRequests += bucket.TotalRequests
		compressed.TotalErrors += bucket.TotalErrors
		compressed.TokensInput += bucket.TokensInput
		compressed.TokensOutput += bucket.TokensOutput
		compressed.PeakRequests = maxInt64(compressed.PeakRequests, bucket.PeakRequests)
		compressed.PeakInputTokens = maxInt64(compressed.PeakInputTokens, bucket.PeakInputTokens)
		compressed.PeakOutputTokens = maxInt64(compressed.PeakOutputTokens, bucket.PeakOutputTokens)
		compressed.DurationSum += bucket.DurationSum
		compressed.DurationCount += bucket.DurationCount

		// Aggregate provider ID metrics
		for id, providerMetrics := range bucket.ByProviderID {
			if compressed.ByProviderID[id] == nil {
				compressed.ByProviderID[id] = &ProviderMetrics{}
			}
			s.mergeProviderMetrics(compressed.ByProviderID[id], providerMetrics)
		}

		// Aggregate provider type metrics
		for providerType, providerMetrics := range bucket.ByProviderType {
			if compressed.ByProviderType[providerType] == nil {
				compressed.ByProviderType[providerType] = &ProviderMetrics{}
			}
			s.mergeProviderMetrics(compressed.ByProviderType[providerType], providerMetrics)
		}

		// Aggregate model metrics
		for model, modelMetrics := range bucket.ByModel {
			if compressed.ByModel[model] == nil {
				compressed.ByModel[model] = &ModelMetrics{}
			}
			s.mergeModelMetrics(compressed.ByModel[model], modelMetrics)
		}

		// Aggregate token metrics
		for tokenID, tokenMetrics := range bucket.ByTokenID {
			if compressed.ByTokenID[tokenID] == nil {
				compressed.ByTokenID[tokenID] = &TokenMetrics{}
			}
			s.mergeTokenMetrics(compressed.ByTokenID[tokenID], tokenMetrics)
		}

		// Aggregate error types
		for errorType, count := range bucket.ErrorsByType {
			compressed.ErrorsByType[errorType] += count
		}
	}

	return compressed
}

// mergeProviderMetrics merges source into destination.
func (s *Service) mergeProviderMetrics(destination, source *ProviderMetrics) {
	destination.Requests += source.Requests
	destination.Errors += source.Errors
	destination.TokensInput += source.TokensInput
	destination.TokensOutput += source.TokensOutput
	destination.PeakRequests = maxInt64(destination.PeakRequests, source.PeakRequests)
	destination.PeakInputTokens = maxInt64(destination.PeakInputTokens, source.PeakInputTokens)
	destination.PeakOutputTokens = maxInt64(destination.PeakOutputTokens, source.PeakOutputTokens)
	destination.DurationSum += source.DurationSum
	destination.DurationCount += source.DurationCount
}

// mergeModelMetrics merges source into destination.
func (s *Service) mergeModelMetrics(destination, source *ModelMetrics) {
	destination.Requests += source.Requests
	destination.Errors += source.Errors
	destination.TokensInput += source.TokensInput
	destination.TokensOutput += source.TokensOutput
	destination.PeakRequests = maxInt64(destination.PeakRequests, source.PeakRequests)
	destination.PeakInputTokens = maxInt64(destination.PeakInputTokens, source.PeakInputTokens)
	destination.PeakOutputTokens = maxInt64(destination.PeakOutputTokens, source.PeakOutputTokens)
}

// mergeTokenMetrics merges source into destination.
func (s *Service) mergeTokenMetrics(destination, source *TokenMetrics) {
	destination.Requests += source.Requests
	destination.Errors += source.Errors
	destination.TokensInput += source.TokensInput
	destination.TokensOutput += source.TokensOutput
	destination.PeakRequests = maxInt64(destination.PeakRequests, source.PeakRequests)
	destination.PeakInputTokens = maxInt64(destination.PeakInputTokens, source.PeakInputTokens)
	destination.PeakOutputTokens = maxInt64(destination.PeakOutputTokens, source.PeakOutputTokens)

	// Keep the most recent LastUsed timestamp
	if source.LastUsed != nil {
		if destination.LastUsed == nil || source.LastUsed.After(*destination.LastUsed) {
			destination.LastUsed = source.LastUsed
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

	s.logger.Debug("compressed old buckets", "from", fromGranularity, "to", toGranularity, "count", len(buckets))
	return nil
}

// loadBucketsByGranularity loads all buckets with a specific granularity older than cutoff.
func (s *Service) loadBucketsByGranularity(cutoff time.Time, granularity string) ([]*MetricBucket, error) {
	var buckets []*MetricBucket

	err := s.database.View(func(tx *bolt.Tx) error {
		metricsBucket := tx.Bucket(db.BucketMetrics)
		if metricsBucket == nil {
			return nil
		}

		cursor := metricsBucket.Cursor()
		for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
			timestamp, granularityValue, err := parseBucketKey(string(key))
			if err != nil {
				continue
			}

			if granularityValue == granularity && timestamp.Before(cutoff) {
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

	return buckets, err
}

// deleteBucket deletes a bucket by key.
func (s *Service) deleteBucket(key string) error {
	return s.database.Update(func(tx *bolt.Tx) error {
		metricsBucket := tx.Bucket(db.BucketMetrics)
		if metricsBucket == nil {
			return nil
		}
		return metricsBucket.Delete([]byte(key))
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
	index := -1
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '_' {
			index = i
			break
		}
	}

	if index == -1 {
		return time.Time{}, "", fmt.Errorf("invalid bucket key format: %s", key)
	}

	timestampString := key[:index]
	granularity := key[index+1:]

	timestamp, err := time.Parse(time.RFC3339, timestampString)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("parse timestamp: %w", err)
	}

	return timestamp, granularity, nil
}
