package metrics

import (
	"context"
	"fmt"
	"time"
)

// compressAndPersist compresses old in-memory buckets and persists them to SQLite.
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
	// Load buckets from SQLite with dimensions so rollups preserve them.
	buckets, err := s.loadBucketsByGranularitySQLite(context.Background(), cutoff, fromGranularity)
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

	// Delete old buckets; dimension children cascade from the base deletes.
	for _, bucket := range buckets {
		if err := s.deleteBucketSQLite(context.Background(), bucket.Timestamp, fromGranularity); err != nil {
			return fmt.Errorf("delete old bucket: %w", err)
		}
	}

	s.logger.Debug("compressed old buckets", "from", fromGranularity, "to", toGranularity, "count", len(buckets))
	return nil
}
