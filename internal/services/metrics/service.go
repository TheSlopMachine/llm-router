// Package metrics implements the Metrics Service for tracking API usage.
package metrics

import (
	"log/slog"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

const (
	eventBufferSize = 10000
	aggregateInterval = 1 * time.Minute
	cleanupInterval = 1 * time.Hour
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
	Timestamp      time.Time                   `json:"timestamp"`
	TotalRequests  int64                       `json:"total_requests"`
	TotalErrors    int64                       `json:"total_errors"`
	TokensInput    int64                       `json:"tokens_input"`
	TokensOutput   int64                       `json:"tokens_output"`
	DurationSum    int64                       `json:"duration_sum"`   // microseconds
	DurationCount  int64                       `json:"duration_count"` // number of requests with duration
	ByProviderID   map[string]*ProviderMetrics `json:"by_provider_id"`
	ByProviderType map[string]*ProviderMetrics `json:"by_provider_type"`
	ByModel        map[string]*ModelMetrics    `json:"by_model"`
	ByTokenID      map[string]*TokenMetrics    `json:"by_token_id"`
	ErrorsByType   map[string]int64            `json:"errors_by_type"`
}

// ProviderMetrics tracks metrics for a specific provider.
type ProviderMetrics struct {
	Requests      int64 `json:"requests"`
	Errors        int64 `json:"errors"`
	TokensInput   int64 `json:"tokens_input"`
	TokensOutput  int64 `json:"tokens_output"`
	DurationSum   int64 `json:"duration_sum"`
	DurationCount int64 `json:"duration_count"`
}

// ModelMetrics tracks metrics for a specific model.
type ModelMetrics struct {
	Requests     int64 `json:"requests"`
	Errors       int64 `json:"errors"`
	TokensInput  int64 `json:"tokens_input"`
	TokensOutput int64 `json:"tokens_output"`
}

// TokenMetrics tracks metrics for a specific token.
type TokenMetrics struct {
	Requests     int64      `json:"requests"`
	Errors       int64      `json:"errors"`
	TokensInput  int64      `json:"tokens_input"`
	TokensOutput int64      `json:"tokens_output"`
	LastUsed     *time.Time `json:"last_used,omitempty"`
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

// Start begins background workers for metrics processing.
func (s *Service) Start() {
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

