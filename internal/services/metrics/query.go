package metrics

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	bolt "go.etcd.io/bbolt"
)

// QueryOverview returns aggregated metrics for the specified filters.
func (s *Service) QueryOverview(filters models.MetricsFilters) (*models.MetricsOverview, error) {
	start, end := filters.TimeRange.Bounds()

	// Fetch buckets from memory and database
	buckets, err := s.fetchBuckets(start, end)
	if err != nil {
		return nil, fmt.Errorf("fetch buckets: %w", err)
	}

	// Apply filters
	filtered := s.applyFilters(buckets, filters)

	// Calculate aggregates
	overview := &models.MetricsOverview{
		TotalRequests:    s.sumRequests(filtered),
		TotalErrors:      s.sumErrors(filtered),
		PeakRequests:     s.calculatePeakRequests(filtered),
		PeakInputTokens:  s.calculatePeakInputTokens(filtered),
		PeakOutputTokens: s.calculatePeakOutputTokens(filtered),
	}

	return overview, nil
}

// QueryTimeSeries returns time-series data for a specific metric.
func (s *Service) QueryTimeSeries(metric string, filters models.MetricsFilters) ([]models.TimeSeriesPoint, error) {
	start, end := filters.TimeRange.Bounds()

	// Fetch buckets
	buckets, err := s.fetchBuckets(start, end)
	if err != nil {
		return nil, fmt.Errorf("fetch buckets: %w", err)
	}

	// Apply filters
	filtered := s.applyFilters(buckets, filters)

	// Sort chronologically for time-scaled rendering
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp.Before(filtered[j].Timestamp)
	})

	// Extract time series based on metric type
	var points []models.TimeSeriesPoint
	for _, bucket := range filtered {
		var value int64
		switch metric {
		case "peak_requests":
			value = bucket.PeakRequests
		case "peak_input_tokens":
			value = bucket.PeakInputTokens
		case "peak_output_tokens":
			value = bucket.PeakOutputTokens
		case "requests":
			value = bucket.TotalRequests
		case "errors":
			value = bucket.TotalErrors
		case "tokens_input":
			value = bucket.TokensInput
		case "tokens_output":
			value = bucket.TokensOutput
		default:
			return nil, fmt.Errorf("unknown metric: %s", metric)
		}

		points = append(points, models.TimeSeriesPoint{
			Timestamp: bucket.Timestamp,
			Value:     value,
		})
	}

	return points, nil
}

// GetDistinctModels returns a list of unique models from metrics data.
func (s *Service) GetDistinctModels() ([]string, error) {
	modelSet := make(map[string]bool)

	// Check in-memory buckets
	s.mu.RLock()
	for _, bucket := range s.recentBuckets {
		for model := range bucket.ByModel {
			modelSet[model] = true
		}
	}
	s.mu.RUnlock()

	// Check database for historical models
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

			for model := range bucket.ByModel {
				modelSet[model] = true
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Convert to slice
	models := make([]string, 0, len(modelSet))
	for model := range modelSet {
		models = append(models, model)
	}

	return models, nil
}

// fetchBuckets retrieves buckets from memory and database for the given time range.
func (s *Service) fetchBuckets(start, end time.Time) ([]*MetricBucket, error) {
	var buckets []*MetricBucket

	// Fetch from in-memory (recent data)
	s.mu.RLock()
	for ts, bucket := range s.recentBuckets {
		if (ts.Equal(start) || ts.After(start)) && (ts.Equal(end) || ts.Before(end)) {
			buckets = append(buckets, bucket)
		}
	}
	s.mu.RUnlock()

	// Fetch from database (historical data)
	dbBuckets, err := s.loadBucketsInRange(start, end)
	if err != nil {
		return nil, err
	}

	buckets = append(buckets, dbBuckets...)
	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].Timestamp.Before(buckets[j].Timestamp)
	})
	return buckets, nil
}

// applyFilters filters buckets based on provider and model.
func (s *Service) applyFilters(buckets []*MetricBucket, filters models.MetricsFilters) []*MetricBucket {
	if filters.ProviderID == "" && filters.Model == "" {
		return buckets
	}

	var filtered []*MetricBucket
	for _, bucket := range buckets {
		filteredBucket := s.filterBucket(bucket, filters)
		if filteredBucket != nil {
			filtered = append(filtered, filteredBucket)
		}
	}

	return filtered
}

// filterBucket creates a filtered copy of a bucket based on filters.
func (s *Service) filterBucket(bucket *MetricBucket, filters models.MetricsFilters) *MetricBucket {
	filtered := &MetricBucket{
		Timestamp:      bucket.Timestamp,
		ByProviderID:   make(map[string]*ProviderMetrics),
		ByProviderType: make(map[string]*ProviderMetrics),
		ByModel:        make(map[string]*ModelMetrics),
		ErrorsByType:   make(map[string]int64),
	}

	// Filter by provider ID
	if filters.ProviderID != "" {
		if pm, exists := bucket.ByProviderID[filters.ProviderID]; exists {
			filtered.TotalRequests = pm.Requests
			filtered.TotalErrors = pm.Errors
			filtered.TokensInput = pm.TokensInput
			filtered.TokensOutput = pm.TokensOutput
			filtered.PeakRequests = pm.PeakRequests
			filtered.PeakInputTokens = pm.PeakInputTokens
			filtered.PeakOutputTokens = pm.PeakOutputTokens
			filtered.DurationSum = pm.DurationSum
			filtered.DurationCount = pm.DurationCount
		} else {
			return nil
		}
	} else {
		filtered.TotalRequests = bucket.TotalRequests
		filtered.TotalErrors = bucket.TotalErrors
		filtered.TokensInput = bucket.TokensInput
		filtered.TokensOutput = bucket.TokensOutput
		filtered.PeakRequests = bucket.PeakRequests
		filtered.PeakInputTokens = bucket.PeakInputTokens
		filtered.PeakOutputTokens = bucket.PeakOutputTokens
		filtered.DurationSum = bucket.DurationSum
		filtered.DurationCount = bucket.DurationCount
	}

	// Filter by model
	if filters.Model != "" {
		if mm, exists := bucket.ByModel[string(filters.Model)]; exists {
			filtered.TotalRequests = mm.Requests
			filtered.TotalErrors = mm.Errors
			filtered.TokensInput = mm.TokensInput
			filtered.TokensOutput = mm.TokensOutput
			filtered.PeakRequests = mm.PeakRequests
			filtered.PeakInputTokens = mm.PeakInputTokens
			filtered.PeakOutputTokens = mm.PeakOutputTokens
		} else {
			return nil
		}
	}

	return filtered
}

// sumRequests calculates total requests across buckets.
func (s *Service) sumRequests(buckets []*MetricBucket) int64 {
	var total int64
	for _, bucket := range buckets {
		total += bucket.TotalRequests
	}
	return total
}

// sumErrors calculates total errors across buckets.
func (s *Service) sumErrors(buckets []*MetricBucket) int64 {
	var total int64
	for _, bucket := range buckets {
		total += bucket.TotalErrors
	}
	return total
}

// calculatePeakRequests calculates true per-minute peak requests (max PeakRequests).
func (s *Service) calculatePeakRequests(buckets []*MetricBucket) int64 {
	var peak int64
	for _, bucket := range buckets {
		if bucket.PeakRequests > peak {
			peak = bucket.PeakRequests
		}
	}
	return peak
}

// calculatePeakInputTokens calculates true per-minute peak input tokens.
func (s *Service) calculatePeakInputTokens(buckets []*MetricBucket) int64 {
	var peak int64
	for _, bucket := range buckets {
		if bucket.PeakInputTokens > peak {
			peak = bucket.PeakInputTokens
		}
	}
	return peak
}

// calculatePeakOutputTokens calculates true per-minute peak output tokens.
func (s *Service) calculatePeakOutputTokens(buckets []*MetricBucket) int64 {
	var peak int64
	for _, bucket := range buckets {
		if bucket.PeakOutputTokens > peak {
			peak = bucket.PeakOutputTokens
		}
	}
	return peak
}

// TokenUsageInfo contains usage statistics for a token.
type TokenUsageInfo struct {
	Requests int64      `json:"requests"`
	LastUsed *time.Time `json:"last_used,omitempty"`
}

// GetTokenUsage returns usage statistics for each token.
func (s *Service) GetTokenUsage() (map[string]*TokenUsageInfo, error) {
	usage := make(map[string]*TokenUsageInfo)

	// Aggregate from in-memory buckets
	s.mu.RLock()
	for _, bucket := range s.recentBuckets {
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
	s.mu.RUnlock()

	// Aggregate from database (all historical data)
	dbUsage, err := s.loadTokenUsageFromDB()
	if err != nil {
		return nil, err
	}

	// Merge database usage
	for tokenID, info := range dbUsage {
		if usage[tokenID] == nil {
			usage[tokenID] = info
		} else {
			usage[tokenID].Requests += info.Requests

			// Keep most recent LastUsed
			if info.LastUsed != nil {
				if usage[tokenID].LastUsed == nil || info.LastUsed.After(*usage[tokenID].LastUsed) {
					usage[tokenID].LastUsed = info.LastUsed
				}
			}
		}
	}

	return usage, nil
}
