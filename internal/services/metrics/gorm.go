package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SQLite flush tuning for the base metric_buckets table.
const (
	// sqliteParameterLimit is the default bound-parameter ceiling.
	sqliteParameterLimit = 32766
	// maxFlushBatchSize caps one CreateInBatches call.
	maxFlushBatchSize = 500
	// metricBucketColumnCount is the scalar column count of record.MetricBucket.
	metricBucketColumnCount = 11
)

// metricBucketBatchSize resolves min(500, floor(32766/columns)) for the base
// bucket rows. Eleven columns give floor(32766/11) = 2978, so the cap wins
// and flushes batch at 500 rows.
func metricBucketBatchSize() int {
	byColumns := sqliteParameterLimit / metricBucketColumnCount
	if byColumns < maxFlushBatchSize {
		return byColumns
	}
	return maxFlushBatchSize
}

// toMetricBucketRecord converts one in-memory bucket to its persistence row.
// Timestamps normalize to UTC. Dimension maps stay in bbolt until Wave 7/8
// fans them out to the child tables.
func toMetricBucketRecord(bucket *MetricBucket, granularity string) record.MetricBucket {
	return record.MetricBucket{
		Granularity:             granularity,
		Timestamp:               bucket.Timestamp.UTC(),
		TotalRequests:           bucket.TotalRequests,
		TotalErrors:             bucket.TotalErrors,
		TokensInput:             bucket.TokensInput,
		TokensOutput:            bucket.TokensOutput,
		PeakRequests:            bucket.PeakRequests,
		PeakInputTokens:         bucket.PeakInputTokens,
		PeakOutputTokens:        bucket.PeakOutputTokens,
		DurationSumMicroseconds: bucket.DurationSum,
		DurationCount:           bucket.DurationCount,
	}
}

// toMetricBucket converts one persistence row to an in-memory bucket.
// Dimension maps stay empty until Wave 7/8 loads the child tables.
func toMetricBucket(row record.MetricBucket) *MetricBucket {
	return &MetricBucket{
		Timestamp:        row.Timestamp,
		TotalRequests:    row.TotalRequests,
		TotalErrors:      row.TotalErrors,
		TokensInput:      row.TokensInput,
		TokensOutput:     row.TokensOutput,
		PeakRequests:     row.PeakRequests,
		PeakInputTokens:  row.PeakInputTokens,
		PeakOutputTokens: row.PeakOutputTokens,
		DurationSum:      row.DurationSumMicroseconds,
		DurationCount:    row.DurationCount,
		ByProviderID:     make(map[string]*ProviderMetrics),
		ByProviderType:   make(map[string]*ProviderMetrics),
		ByModel:          make(map[string]*ModelMetrics),
		ByTokenID:        make(map[string]*TokenMetrics),
		ErrorsByType:     make(map[string]int64),
	}
}

// upsertMetricBucketRecords writes base bucket rows in one transaction.
// Primary key conflicts overwrite every column. A nil handle is a no-op so
// bbolt-only operation needs no call-site branching.
func (s *Service) upsertMetricBucketRecords(ctx context.Context, rows []record.MetricBucket) error {
	sqliteDatabase := s.sqliteDatabase
	if sqliteDatabase == nil || len(rows) == 0 {
		return nil
	}
	if err := sqliteDatabase.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if err := gorm.G[record.MetricBucket](transaction, clause.OnConflict{
			Columns:   []clause.Column{{Name: "granularity"}, {Name: "timestamp"}},
			UpdateAll: true,
		}).CreateInBatches(ctx, &rows, metricBucketBatchSize()); err != nil {
			return fmt.Errorf("upsert metric buckets: %w", db.Translate(err))
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

// persistBucketSQLite dual-writes one base bucket row plus its dimension
// children. Dimension writes are stubs until Wave 7/8.
func (s *Service) persistBucketSQLite(ctx context.Context, bucket *MetricBucket, granularity string) error {
	if s.sqliteDatabase == nil || bucket == nil {
		return nil
	}
	if err := s.upsertMetricBucketRecords(ctx, []record.MetricBucket{toMetricBucketRecord(bucket, granularity)}); err != nil {
		return err
	}
	return s.persistMetricBucketDimensions(ctx, bucket, granularity)
}

// loadBucketSQLite loads one base bucket row by primary key.
func (s *Service) loadBucketSQLite(ctx context.Context, timestamp time.Time, granularity string) (*MetricBucket, error) {
	if s.bucketRepository == nil {
		return nil, fmt.Errorf("sqlite database is not attached")
	}
	row, err := s.bucketRepository.Get(ctx,
		clause.Eq{Column: "granularity", Value: granularity},
		clause.Eq{Column: "timestamp", Value: timestamp.UTC()},
	)
	if err != nil {
		return nil, err
	}
	return toMetricBucket(row), nil
}

// loadBucketsInRangeSQLite loads every base bucket row within the closed
// interval, across all granularities, matching the bbolt scan semantics.
func (s *Service) loadBucketsInRangeSQLite(ctx context.Context, start, end time.Time) ([]*MetricBucket, error) {
	if s.bucketRepository == nil {
		return nil, nil
	}
	rows, err := s.bucketRepository.List(ctx,
		clause.Gte{Column: "timestamp", Value: start.UTC()},
		clause.Lte{Column: "timestamp", Value: end.UTC()},
	)
	if err != nil {
		return nil, err
	}
	buckets := make([]*MetricBucket, 0, len(rows))
	for _, row := range rows {
		buckets = append(buckets, toMetricBucket(row))
	}
	return buckets, nil
}

// loadBucketsByGranularitySQLite loads base bucket rows of one granularity
// older than the cutoff for compression.
func (s *Service) loadBucketsByGranularitySQLite(ctx context.Context, cutoff time.Time, granularity string) ([]*MetricBucket, error) {
	if s.bucketRepository == nil {
		return nil, nil
	}
	rows, err := s.bucketRepository.List(ctx,
		clause.Eq{Column: "granularity", Value: granularity},
		clause.Lt{Column: "timestamp", Value: cutoff.UTC()},
	)
	if err != nil {
		return nil, err
	}
	buckets := make([]*MetricBucket, 0, len(rows))
	for _, row := range rows {
		buckets = append(buckets, toMetricBucket(row))
	}
	return buckets, nil
}

// deleteBucketsOlderThanSQLite removes base bucket rows older than the
// cutoff. The foreign key cascade removes the dimension children.
func (s *Service) deleteBucketsOlderThanSQLite(ctx context.Context, cutoff time.Time) (int64, error) {
	if s.bucketRepository == nil {
		return 0, nil
	}
	removed, err := s.bucketRepository.Delete(ctx, clause.Lt{Column: "timestamp", Value: cutoff.UTC()})
	if err != nil {
		return 0, err
	}
	if removed > 0 {
		s.logger.Info("deleted old metric buckets from sqlite", "count", removed)
	}
	return removed, nil
}

// deleteBucketSQLite removes one base bucket row by primary key. The foreign
// key cascade removes its dimension children.
func (s *Service) deleteBucketSQLite(ctx context.Context, timestamp time.Time, granularity string) error {
	if s.bucketRepository == nil {
		return nil
	}
	_, err := s.bucketRepository.Delete(ctx,
		clause.Eq{Column: "granularity", Value: granularity},
		clause.Eq{Column: "timestamp", Value: timestamp.UTC()},
	)
	return err
}

// mergeSQLiteBuckets unions bbolt and SQLite rows by UTC timestamp. SQLite
// wins on collision because it carries the current dual-write state. The
// in-memory bucket carries no granularity, so cross-granularity rows sharing
// one timestamp collapse; Wave 7/8 restores granularity-aware merging with
// the dimension load.
func mergeSQLiteBuckets(stored []*MetricBucket, sqliteBuckets []*MetricBucket) []*MetricBucket {
	if len(sqliteBuckets) == 0 {
		return stored
	}
	merged := make([]*MetricBucket, 0, len(stored)+len(sqliteBuckets))
	overridden := make(map[string]bool, len(sqliteBuckets))
	for _, bucket := range sqliteBuckets {
		overridden[bucket.Timestamp.UTC().Format(time.RFC3339)] = true
	}
	for _, bucket := range stored {
		if !overridden[bucket.Timestamp.UTC().Format(time.RFC3339)] {
			merged = append(merged, bucket)
		}
	}
	return append(merged, sqliteBuckets...)
}

// persistMetricBucketDimensions fans out dimension child rows for one bucket.
// TODO(Wave 7/8): implement provider, model, token, and error-type writes.
func (s *Service) persistMetricBucketDimensions(ctx context.Context, bucket *MetricBucket, granularity string) error {
	if s.sqliteDatabase == nil {
		return nil
	}
	if err := s.persistMetricBucketProviders(ctx, bucket, granularity); err != nil {
		return err
	}
	if err := s.persistMetricBucketModels(ctx, bucket, granularity); err != nil {
		return err
	}
	if err := s.persistMetricBucketTokens(ctx, bucket, granularity); err != nil {
		return err
	}
	return s.persistMetricBucketErrorTypes(ctx, bucket, granularity)
}

// persistMetricBucketProviders writes metric_bucket_providers rows for both
// dimensions (provider_id and provider_type).
// TODO(Wave 7/8): implement provider dimension writes.
func (s *Service) persistMetricBucketProviders(_ context.Context, _ *MetricBucket, _ string) error {
	return nil
}

// persistMetricBucketModels writes metric_bucket_models rows.
// TODO(Wave 7/8): implement model dimension writes.
func (s *Service) persistMetricBucketModels(_ context.Context, _ *MetricBucket, _ string) error {
	return nil
}

// persistMetricBucketTokens writes metric_bucket_tokens rows.
// TODO(Wave 7/8): implement token dimension writes.
func (s *Service) persistMetricBucketTokens(_ context.Context, _ *MetricBucket, _ string) error {
	return nil
}

// persistMetricBucketErrorTypes writes metric_bucket_error_types rows.
// TODO(Wave 7/8): implement error-type dimension writes.
func (s *Service) persistMetricBucketErrorTypes(_ context.Context, _ *MetricBucket, _ string) error {
	return nil
}
