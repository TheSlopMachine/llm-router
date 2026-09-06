// Package metrics implements the Metrics Service for tracking API usage.
package metrics

import (
	"log/slog"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	bolt "go.etcd.io/bbolt"
)

const (
	eventBufferSize   = 10000
	aggregateInterval = 1 * time.Minute
	cleanupInterval   = 1 * time.Hour
	metricsVersion    = "2"
)

// Service manages metrics collection, aggregation, and querying.
type Service struct {
	db     *db.DB
	logger *slog.Logger
	mu     sync.RWMutex

	// In-memory ring buffer: map[timestamp]*MetricBucket
	recentBuckets map[time.Time]*MetricBucket

	// Background workers
	aggregatorTicker *time.Ticker
	cleanupTicker    *time.Ticker
	stopCh           chan struct{}

	// Non-blocking event channel
	eventCh chan models.MetricEvent
}

// MetricBucket represents aggregated metrics for a time window.
type MetricBucket struct {
	Timestamp        time.Time                   `json:"timestamp"`
	TotalRequests    int64                       `json:"total_requests"`
	TotalErrors      int64                       `json:"total_errors"`
	TokensInput      int64                       `json:"tokens_input"`
	TokensOutput     int64                       `json:"tokens_output"`
	PeakRequests     int64                       `json:"peak_requests"`
	PeakInputTokens  int64                       `json:"peak_input_tokens"`
	PeakOutputTokens int64                       `json:"peak_output_tokens"`
	DurationSum      int64                       `json:"duration_sum"`   // microseconds
	DurationCount    int64                       `json:"duration_count"` // number of requests with duration
	ByProviderID     map[string]*ProviderMetrics `json:"by_provider_id"`
	ByProviderType   map[string]*ProviderMetrics `json:"by_provider_type"`
	ByModel          map[string]*ModelMetrics    `json:"by_model"`
	ByTokenID        map[string]*TokenMetrics    `json:"by_token_id"`
	ErrorsByType     map[string]int64            `json:"errors_by_type"`
}

// ProviderMetrics tracks metrics for a specific provider.
type ProviderMetrics struct {
	Requests         int64 `json:"requests"`
	Errors           int64 `json:"errors"`
	TokensInput      int64 `json:"tokens_input"`
	TokensOutput     int64 `json:"tokens_output"`
	PeakRequests     int64 `json:"peak_requests"`
	PeakInputTokens  int64 `json:"peak_input_tokens"`
	PeakOutputTokens int64 `json:"peak_output_tokens"`
	DurationSum      int64 `json:"duration_sum"`
	DurationCount    int64 `json:"duration_count"`
}

// ModelMetrics tracks metrics for a specific model.
type ModelMetrics struct {
	Requests         int64 `json:"requests"`
	Errors           int64 `json:"errors"`
	TokensInput      int64 `json:"tokens_input"`
	TokensOutput     int64 `json:"tokens_output"`
	PeakRequests     int64 `json:"peak_requests"`
	PeakInputTokens  int64 `json:"peak_input_tokens"`
	PeakOutputTokens int64 `json:"peak_output_tokens"`
}

// TokenMetrics tracks metrics for a specific token.
type TokenMetrics struct {
	Requests         int64      `json:"requests"`
	Errors           int64      `json:"errors"`
	TokensInput      int64      `json:"tokens_input"`
	TokensOutput     int64      `json:"tokens_output"`
	PeakRequests     int64      `json:"peak_requests"`
	PeakInputTokens  int64      `json:"peak_input_tokens"`
	PeakOutputTokens int64      `json:"peak_output_tokens"`
	LastUsed         *time.Time `json:"last_used,omitempty"`
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// New constructs a new metrics Service.
func New(database *db.DB, logger *slog.Logger) *Service {
	return &Service{
		db:            database,
		logger:        logger,
		recentBuckets: make(map[time.Time]*MetricBucket),
		eventCh:       make(chan models.MetricEvent, eventBufferSize),
		stopCh:        make(chan struct{}),
	}
}

// ensureCleanMetrics wipes legacy buckets if metrics_version is stale.
func (s *Service) ensureCleanMetrics() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(db.BucketMeta)
		if meta == nil {
			return nil
		}
		if string(meta.Get([]byte("metrics_version"))) == metricsVersion {
			return nil
		}
		b := tx.Bucket(db.BucketMetrics)
		if b != nil {
			c := b.Cursor()
			for k, _ := c.First(); k != nil; k, _ = c.Next() {
				if err := b.Delete(k); err != nil {
					return err
				}
			}
		}
		if err := meta.Put([]byte("metrics_version"), []byte(metricsVersion)); err != nil {
			return err
		}
		s.logger.Info("metrics wiped for new version", "version", metricsVersion)
		return nil
	})
}

// Start begins background workers for metrics processing.
func (s *Service) Start() {
	if err := s.ensureCleanMetrics(); err != nil {
		s.logger.Error("metrics version check failed", "err", err)
	}
	go s.processEvents()
	go s.startAggregator()
	go s.startCleanup()
	s.logger.Info("metrics service started")
}

// Stop gracefully shuts down the metrics service.
func (s *Service) Stop() {
	close(s.stopCh)
	if s.aggregatorTicker != nil {
		s.aggregatorTicker.Stop()
	}
	if s.cleanupTicker != nil {
		s.cleanupTicker.Stop()
	}
	s.logger.Info("metrics service stopped")
}

// RecordRequest records a metric event (non-blocking).
func (s *Service) RecordRequest(event models.MetricEvent) {
	select {
	case s.eventCh <- event:
		// Event queued successfully
	default:
		// Buffer full, drop event with warning
		s.logger.Warn("metrics event buffer full, dropping event")
	}
}

// processEvents consumes events from the channel and updates buckets.
func (s *Service) processEvents() {
	for {
		select {
		case event := <-s.eventCh:
			s.recordEvent(event)
		case <-s.stopCh:
			return
		}
	}
}

// recordEvent updates the appropriate bucket with the event data.
func (s *Service) recordEvent(event models.MetricEvent) {
	// Round timestamp to minute boundary
	bucketTime := event.Timestamp.Truncate(time.Minute)

	s.mu.Lock()
	defer s.mu.Unlock()

	bucket, exists := s.recentBuckets[bucketTime]
	if !exists {
		bucket = &MetricBucket{
			Timestamp:      bucketTime,
			ByProviderID:   make(map[string]*ProviderMetrics),
			ByProviderType: make(map[string]*ProviderMetrics),
			ByModel:        make(map[string]*ModelMetrics),
			ByTokenID:      make(map[string]*TokenMetrics),
			ErrorsByType:   make(map[string]int64),
		}
		s.recentBuckets[bucketTime] = bucket
	}

	// Update totals
	bucket.TotalRequests++
	bucket.TokensInput += event.TokensInput
	bucket.TokensOutput += event.TokensOutput
	if event.Duration > 0 {
		bucket.DurationSum += event.Duration.Microseconds()
		bucket.DurationCount++
	}
	// Peak per-minute = max per-minute bucket totals within window (1m buckets: peak == total)
	bucket.PeakRequests = maxInt64(bucket.PeakRequests, bucket.TotalRequests)
	bucket.PeakInputTokens = maxInt64(bucket.PeakInputTokens, bucket.TokensInput)
	bucket.PeakOutputTokens = maxInt64(bucket.PeakOutputTokens, bucket.TokensOutput)

	// Track errors
	if event.ErrorType != "" {
		bucket.TotalErrors++
		bucket.ErrorsByType[event.ErrorType]++
	}

	// Update provider ID metrics
	if event.ProviderID != "" {
		pm := bucket.ByProviderID[event.ProviderID]
		if pm == nil {
			pm = &ProviderMetrics{}
			bucket.ByProviderID[event.ProviderID] = pm
		}
		s.updateProviderMetrics(pm, event)
	}

	// Update provider type metrics
	if event.ProviderType != "" {
		pm := bucket.ByProviderType[event.ProviderType]
		if pm == nil {
			pm = &ProviderMetrics{}
			bucket.ByProviderType[event.ProviderType] = pm
		}
		s.updateProviderMetrics(pm, event)
	}

	// Update model metrics
	if event.Model != "" {
		mm := bucket.ByModel[string(event.Model)]
		if mm == nil {
			mm = &ModelMetrics{}
			bucket.ByModel[string(event.Model)] = mm
		}
		mm.Requests++
		mm.TokensInput += event.TokensInput
		mm.TokensOutput += event.TokensOutput
		mm.PeakRequests = maxInt64(mm.PeakRequests, mm.Requests)
		mm.PeakInputTokens = maxInt64(mm.PeakInputTokens, mm.TokensInput)
		mm.PeakOutputTokens = maxInt64(mm.PeakOutputTokens, mm.TokensOutput)
		if event.ErrorType != "" {
			mm.Errors++
		}
	}

	// Update token metrics
	if event.TokenID != "" {
		tm := bucket.ByTokenID[event.TokenID]
		if tm == nil {
			tm = &TokenMetrics{}
			bucket.ByTokenID[event.TokenID] = tm
		}
		tm.Requests++
		tm.TokensInput += event.TokensInput
		tm.TokensOutput += event.TokensOutput
		tm.PeakRequests = maxInt64(tm.PeakRequests, tm.Requests)
		tm.PeakInputTokens = maxInt64(tm.PeakInputTokens, tm.TokensInput)
		tm.PeakOutputTokens = maxInt64(tm.PeakOutputTokens, tm.TokensOutput)

		// Update last used timestamp
		eventTime := event.Timestamp
		tm.LastUsed = &eventTime

		if event.ErrorType != "" {
			tm.Errors++
		}
	}
}

// updateProviderMetrics updates provider metrics with event data.
func (s *Service) updateProviderMetrics(pm *ProviderMetrics, event models.MetricEvent) {
	pm.Requests++
	pm.TokensInput += event.TokensInput
	pm.TokensOutput += event.TokensOutput
	pm.PeakRequests = maxInt64(pm.PeakRequests, pm.Requests)
	pm.PeakInputTokens = maxInt64(pm.PeakInputTokens, pm.TokensInput)
	pm.PeakOutputTokens = maxInt64(pm.PeakOutputTokens, pm.TokensOutput)
	if event.Duration > 0 {
		pm.DurationSum += event.Duration.Microseconds()
		pm.DurationCount++
	}
	if event.ErrorType != "" {
		pm.Errors++
	}
}

// startAggregator runs periodic aggregation and persistence.
func (s *Service) startAggregator() {
	s.aggregatorTicker = time.NewTicker(aggregateInterval)
	defer s.aggregatorTicker.Stop()

	for {
		select {
		case <-s.aggregatorTicker.C:
			if err := s.compressAndPersist(); err != nil {
				s.logger.Error("aggregation failed", "err", err)
			}
		case <-s.stopCh:
			return
		}
	}
}

// startCleanup runs periodic cleanup of old data.
func (s *Service) startCleanup() {
	s.cleanupTicker = time.NewTicker(cleanupInterval)
	defer s.cleanupTicker.Stop()

	for {
		select {
		case <-s.cleanupTicker.C:
			if err := s.cleanup(); err != nil {
				s.logger.Error("cleanup failed", "err", err)
			}
		case <-s.stopCh:
			return
		}
	}
}

// cleanup performs data compression and deletion of old data.
func (s *Service) cleanup() error {
	now := time.Now()

	// Compress 30-minute buckets older than 1 day into 2-hour buckets
	if err := s.compressOldBuckets(now.Add(-24*time.Hour), "30m", "2h", 2*time.Hour); err != nil {
		return err
	}

	// Compress 2-hour buckets older than 7 days into 6-hour buckets
	if err := s.compressOldBuckets(now.Add(-7*24*time.Hour), "2h", "6h", 6*time.Hour); err != nil {
		return err
	}

	// Delete all buckets older than 90 days
	cutoff := now.Add(-90 * 24 * time.Hour)
	if err := s.deleteBucketsOlderThan(cutoff); err != nil {
		return err
	}

	return nil
}
