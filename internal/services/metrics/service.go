// Package metrics implements the Metrics Service for tracking API usage.
package metrics

import (
	"log/slog"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"gorm.io/gorm"
)

const (
	eventBufferSize   = 10000
	aggregateInterval = 1 * time.Minute
	cleanupInterval   = 1 * time.Hour
)

// Service manages metrics collection, aggregation, and querying on the
// cache SQLite handle.
type Service struct {
	// sqliteDatabase is the cache SQLite handle. Only the base handle is
	// stored, never a chained query.
	sqliteDatabase *gorm.DB
	// bucketRepository serves typed base-bucket reads and retention deletes.
	bucketRepository *repository.GormRepository[record.MetricBucket]
	logger           *slog.Logger
	mutex            sync.RWMutex

	// In-memory ring buffer: map[timestamp]*MetricBucket
	recentBuckets map[time.Time]*MetricBucket

	// Background workers
	aggregatorTicker *time.Ticker
	cleanupTicker    *time.Ticker
	stopChannel      chan struct{}
	stopOnce         sync.Once
	waitGroup        sync.WaitGroup

	// Non-blocking event channel
	eventChannel chan models.MetricEvent
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

// New constructs a new metrics Service on the cache SQLite handle.
func New(sqliteDatabase *db.Database, logger *slog.Logger) *Service {
	service := &Service{
		logger:        logger,
		recentBuckets: make(map[time.Time]*MetricBucket),
		eventChannel:  make(chan models.MetricEvent, eventBufferSize),
		stopChannel:   make(chan struct{}),
	}
	if sqliteDatabase != nil && sqliteDatabase.Cache != nil {
		service.sqliteDatabase = sqliteDatabase.Cache
		service.bucketRepository = repository.NewGormRepository[record.MetricBucket](sqliteDatabase.Cache, "granularity", "timestamp")
	}
	return service
}

// Start begins background workers for metrics processing.
func (s *Service) Start() {
	s.waitGroup.Add(3)
	go s.processEvents()
	go s.startAggregator()
	go s.startCleanup()
	s.logger.Info("metrics service started")
}

// Stop gracefully shuts down the metrics service: workers finish, queued
// events drain into buckets, and all in-memory buckets flush to storage.
func (s *Service) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopChannel)
		if s.aggregatorTicker != nil {
			s.aggregatorTicker.Stop()
		}
		if s.cleanupTicker != nil {
			s.cleanupTicker.Stop()
		}
		s.waitGroup.Wait()
		s.drainEvents()
		if err := s.flushAll(); err != nil {
			s.logger.Error("metrics final flush failed", "err", err)
		}
		s.logger.Info("metrics service stopped")
	})
}

// drainEvents folds every queued event into its bucket after workers exit.
func (s *Service) drainEvents() {
	for {
		select {
		case event := <-s.eventChannel:
			s.recordEvent(event)
		default:
			return
		}
	}
}

// flushAll persists every in-memory bucket without the 1h age cutoff used
// by periodic aggregation. Buckets leave memory only after a durable write.
func (s *Service) flushAll() error {
	s.mutex.Lock()
	pending := make([]*MetricBucket, 0, len(s.recentBuckets))
	stamps := make([]time.Time, 0, len(s.recentBuckets))
	for timestamp, bucket := range s.recentBuckets {
		pending = append(pending, bucket)
		stamps = append(stamps, timestamp)
	}
	s.mutex.Unlock()
	for i, bucket := range pending {
		if err := s.persistBucket(bucket, "1m"); err != nil {
			return err
		}
		s.mutex.Lock()
		delete(s.recentBuckets, stamps[i])
		s.mutex.Unlock()
	}
	return nil
}

// RecordRequest records a metric event (non-blocking).
func (s *Service) RecordRequest(event models.MetricEvent) {
	select {
	case s.eventChannel <- event:
		// Event queued successfully
	default:
		// Buffer full, drop event with warning
		s.logger.Warn("metrics event buffer full, dropping event")
	}
}

// processEvents consumes events from the channel and updates buckets.
func (s *Service) processEvents() {
	defer s.waitGroup.Done()
	for {
		select {
		case event := <-s.eventChannel:
			s.recordEvent(event)
		case <-s.stopChannel:
			return
		}
	}
}

// recordEvent updates the appropriate bucket with the event data.
func (s *Service) recordEvent(event models.MetricEvent) {
	// Round timestamp to minute boundary
	bucketTime := event.Timestamp.Truncate(time.Minute)

	s.mutex.Lock()
	defer s.mutex.Unlock()

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
		providerMetrics := bucket.ByProviderID[event.ProviderID]
		if providerMetrics == nil {
			providerMetrics = &ProviderMetrics{}
			bucket.ByProviderID[event.ProviderID] = providerMetrics
		}
		s.updateProviderMetrics(providerMetrics, event)
	}

	// Update provider type metrics
	if event.ProviderType != "" {
		providerMetrics := bucket.ByProviderType[event.ProviderType]
		if providerMetrics == nil {
			providerMetrics = &ProviderMetrics{}
			bucket.ByProviderType[event.ProviderType] = providerMetrics
		}
		s.updateProviderMetrics(providerMetrics, event)
	}

	// Update model metrics
	if event.Model != "" {
		modelMetrics := bucket.ByModel[string(event.Model)]
		if modelMetrics == nil {
			modelMetrics = &ModelMetrics{}
			bucket.ByModel[string(event.Model)] = modelMetrics
		}
		modelMetrics.Requests++
		modelMetrics.TokensInput += event.TokensInput
		modelMetrics.TokensOutput += event.TokensOutput
		modelMetrics.PeakRequests = maxInt64(modelMetrics.PeakRequests, modelMetrics.Requests)
		modelMetrics.PeakInputTokens = maxInt64(modelMetrics.PeakInputTokens, modelMetrics.TokensInput)
		modelMetrics.PeakOutputTokens = maxInt64(modelMetrics.PeakOutputTokens, modelMetrics.TokensOutput)
		if event.ErrorType != "" {
			modelMetrics.Errors++
		}
	}

	// Update token metrics
	if event.TokenID != "" {
		tokenMetrics := bucket.ByTokenID[event.TokenID]
		if tokenMetrics == nil {
			tokenMetrics = &TokenMetrics{}
			bucket.ByTokenID[event.TokenID] = tokenMetrics
		}
		tokenMetrics.Requests++
		tokenMetrics.TokensInput += event.TokensInput
		tokenMetrics.TokensOutput += event.TokensOutput
		tokenMetrics.PeakRequests = maxInt64(tokenMetrics.PeakRequests, tokenMetrics.Requests)
		tokenMetrics.PeakInputTokens = maxInt64(tokenMetrics.PeakInputTokens, tokenMetrics.TokensInput)
		tokenMetrics.PeakOutputTokens = maxInt64(tokenMetrics.PeakOutputTokens, tokenMetrics.TokensOutput)

		// Update last used timestamp
		eventTime := event.Timestamp
		tokenMetrics.LastUsed = &eventTime

		if event.ErrorType != "" {
			tokenMetrics.Errors++
		}
	}
}

// updateProviderMetrics updates provider metrics with event data.
func (s *Service) updateProviderMetrics(providerMetrics *ProviderMetrics, event models.MetricEvent) {
	providerMetrics.Requests++
	providerMetrics.TokensInput += event.TokensInput
	providerMetrics.TokensOutput += event.TokensOutput
	providerMetrics.PeakRequests = maxInt64(providerMetrics.PeakRequests, providerMetrics.Requests)
	providerMetrics.PeakInputTokens = maxInt64(providerMetrics.PeakInputTokens, providerMetrics.TokensInput)
	providerMetrics.PeakOutputTokens = maxInt64(providerMetrics.PeakOutputTokens, providerMetrics.TokensOutput)
	if event.Duration > 0 {
		providerMetrics.DurationSum += event.Duration.Microseconds()
		providerMetrics.DurationCount++
	}
	if event.ErrorType != "" {
		providerMetrics.Errors++
	}
}

// startAggregator runs periodic aggregation and persistence.
func (s *Service) startAggregator() {
	defer s.waitGroup.Done()
	s.aggregatorTicker = time.NewTicker(aggregateInterval)
	defer s.aggregatorTicker.Stop()

	for {
		select {
		case <-s.aggregatorTicker.C:
			if err := s.compressAndPersist(); err != nil {
				s.logger.Error("aggregation failed", "err", err)
			}
		case <-s.stopChannel:
			return
		}
	}
}

// startCleanup runs periodic cleanup of old data.
func (s *Service) startCleanup() {
	defer s.waitGroup.Done()
	s.cleanupTicker = time.NewTicker(cleanupInterval)
	defer s.cleanupTicker.Stop()

	for {
		select {
		case <-s.cleanupTicker.C:
			if err := s.cleanup(); err != nil {
				s.logger.Error("cleanup failed", "err", err)
			}
		case <-s.stopChannel:
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
