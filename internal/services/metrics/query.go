package metrics

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
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
		TotalRequests:  s.sumRequests(filtered),
		TotalErrors:    s.sumErrors(filtered),
		PeakRPM:        s.calculatePeakRPM(filtered),
		PeakTPMInput:   s.calculatePeakTPM(filtered, "input"),
		PeakTPMOutput:  s.calculatePeakTPM(filtered, "output"),
		PeakRPD:        s.calculatePeakRPD(filtered),
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

	// Extract time series based on metric type
	var points []models.TimeSeriesPoint
	for _, bucket := range filtered {
		var value int64
		switch metric {
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

// calculatePeakRPM calculates peak requests per minute.
func (s *Service) calculatePeakRPM(buckets []*MetricBucket) int64 {
	var peak int64
	for _, bucket := range buckets {
		if bucket.TotalRequests > peak {
			peak = bucket.TotalRequests
		}
	}
	return peak
}

// calculatePeakTPM calculates peak tokens per minute.
func (s *Service) calculatePeakTPM(buckets []*MetricBucket, tokenType string) int64 {
	var peak int64
	for _, bucket := range buckets {
		var tokens int64
		if tokenType == "input" {
			tokens = bucket.TokensInput
		} else {
			tokens = bucket.TokensOutput
		}
		if tokens > peak {
			peak = tokens
		}
	}
	return peak
}

// calculatePeakRPD calculates peak requests per day.
func (s *Service) calculatePeakRPD(buckets []*MetricBucket) int64 {
	// Group buckets by day
	dailyRequests := make(map[string]int64)
	for _, bucket := range buckets {
		day := bucket.Timestamp.Format("2006-01-02")
		dailyRequests[day] += bucket.TotalRequests
	}

	// Find peak
	var peak int64
	for _, requests := range dailyRequests {
		if requests > peak {
			peak = requests
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

