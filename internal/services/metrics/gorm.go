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
// Timestamps normalize to UTC. Dimensions fan out to the child tables in
// persistMetricBucketDimensions.
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
// Dimensions attach separately through attachBucketDimensions.
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

// persistBucketSQLite writes one base bucket row plus its dimension
// children in one transaction on the transaction handle only.
func (s *Service) persistBucketSQLite(ctx context.Context, bucket *MetricBucket, granularity string) error {
	if s.sqliteDatabase == nil || bucket == nil {
		return nil
	}
	return s.sqliteDatabase.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		rows := []record.MetricBucket{toMetricBucketRecord(bucket, granularity)}
		if err := gorm.G[record.MetricBucket](transaction, clause.OnConflict{
			Columns:   []clause.Column{{Name: "granularity"}, {Name: "timestamp"}},
			UpdateAll: true,
		}).CreateInBatches(ctx, &rows, metricBucketBatchSize()); err != nil {
			return fmt.Errorf("upsert metric buckets: %w", db.Translate(err))
		}
		return s.persistMetricBucketDimensions(ctx, transaction, bucket, granularity)
	})
}

// loadBucketSQLite loads one base bucket row by primary key with its
// dimension children attached.
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
	bucket := toMetricBucket(row)
	if err := s.attachBucketDimensions(ctx, s.sqliteDatabase, []record.MetricBucket{row}, []*MetricBucket{bucket}); err != nil {
		return nil, err
	}
	return bucket, nil
}

// loadBucketsInRangeSQLite loads every base bucket row within the closed
// interval, across all granularities, with dimension children attached.
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
	if err := s.attachBucketDimensions(ctx, s.sqliteDatabase, rows, buckets); err != nil {
		return nil, err
	}
	return buckets, nil
}

// loadBucketsByGranularitySQLite loads base bucket rows of one granularity
// older than the cutoff for compression, with dimension children attached
// so rollups preserve the breakdowns.
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
	if err := s.attachBucketDimensions(ctx, s.sqliteDatabase, rows, buckets); err != nil {
		return nil, err
	}
	return buckets, nil
}

// attachBucketDimensions loads the dimension children of the given base
// rows in four timestamp-scoped bulk queries and attaches them to the
// converted buckets. Rows and buckets run in parallel order; children match
// on the bucket granularity plus the UTC timestamp. Timestamps are
// minute-aligned on every granularity, so the second-precision key is exact.
func (s *Service) attachBucketDimensions(ctx context.Context, handle *gorm.DB, rows []record.MetricBucket, buckets []*MetricBucket) error {
	if len(rows) == 0 {
		return nil
	}
	byBucket := make(map[string]*MetricBucket, len(buckets))
	granularity := rows[0].Granularity
	singleGranularity := true
	earliest := rows[0].Timestamp.UTC()
	latest := earliest
	for index, row := range rows {
		stamp := row.Timestamp.UTC()
		if earliest.After(stamp) {
			earliest = stamp
		}
		if latest.Before(stamp) {
			latest = stamp
		}
		if row.Granularity != granularity {
			singleGranularity = false
		}
		byBucket[dimensionBucketKey(row.Granularity, stamp)] = buckets[index]
	}
	providerConditions := []clause.Expression{
		clause.Gte{Column: "timestamp", Value: earliest},
		clause.Lte{Column: "timestamp", Value: latest},
	}
	modelConditions := append([]clause.Expression(nil), providerConditions...)
	tokenConditions := append([]clause.Expression(nil), providerConditions...)
	errorConditions := append([]clause.Expression(nil), providerConditions...)
	if singleGranularity {
		providerConditions = append(providerConditions, clause.Eq{Column: "granularity", Value: granularity})
		modelConditions = append(modelConditions, clause.Eq{Column: "granularity", Value: granularity})
		tokenConditions = append(tokenConditions, clause.Eq{Column: "granularity", Value: granularity})
		errorConditions = append(errorConditions, clause.Eq{Column: "granularity", Value: granularity})
	}
	providers, err := gorm.G[record.MetricBucketProvider](handle, providerConditions...).Find(ctx)
	if err != nil {
		return db.Translate(err)
	}
	for _, child := range providers {
		if bucket, ok := byBucket[dimensionBucketKey(child.Granularity, child.Timestamp)]; ok {
			entry := &ProviderMetrics{
				Requests: child.Requests, Errors: child.Errors,
				TokensInput: child.TokensInput, TokensOutput: child.TokensOutput,
				PeakRequests: child.PeakRequests, PeakInputTokens: child.PeakInputTokens, PeakOutputTokens: child.PeakOutputTokens,
			}
			switch child.Dimension {
			case "provider_id":
				if bucket.ByProviderID == nil {
					bucket.ByProviderID = make(map[string]*ProviderMetrics)
				}
				bucket.ByProviderID[child.Key] = entry
			case "provider_type":
				if bucket.ByProviderType == nil {
					bucket.ByProviderType = make(map[string]*ProviderMetrics)
				}
				bucket.ByProviderType[child.Key] = entry
			}
		}
	}
	modelRows, err := gorm.G[record.MetricBucketModel](handle, modelConditions...).Find(ctx)
	if err != nil {
		return db.Translate(err)
	}
	for _, child := range modelRows {
		if bucket, ok := byBucket[dimensionBucketKey(child.Granularity, child.Timestamp)]; ok {
			if bucket.ByModel == nil {
				bucket.ByModel = make(map[string]*ModelMetrics)
			}
			bucket.ByModel[child.ModelID] = &ModelMetrics{
				Requests: child.Requests, Errors: child.Errors,
				TokensInput: child.TokensInput, TokensOutput: child.TokensOutput,
				PeakRequests: child.PeakRequests, PeakInputTokens: child.PeakInputTokens, PeakOutputTokens: child.PeakOutputTokens,
			}
		}
	}
	tokenRows, err := gorm.G[record.MetricBucketToken](handle, tokenConditions...).Find(ctx)
	if err != nil {
		return db.Translate(err)
	}
	for _, child := range tokenRows {
		if bucket, ok := byBucket[dimensionBucketKey(child.Granularity, child.Timestamp)]; ok {
			if bucket.ByTokenID == nil {
				bucket.ByTokenID = make(map[string]*TokenMetrics)
			}
			bucket.ByTokenID[child.TokenID] = &TokenMetrics{
				Requests: child.Requests, Errors: child.Errors,
				TokensInput: child.TokensInput, TokensOutput: child.TokensOutput,
				PeakRequests: child.PeakRequests, PeakInputTokens: child.PeakInputTokens, PeakOutputTokens: child.PeakOutputTokens,
				LastUsed: child.LastUsed,
			}
		}
	}
	errorRows, err := gorm.G[record.MetricBucketErrorType](handle, errorConditions...).Find(ctx)
	if err != nil {
		return db.Translate(err)
	}
	for _, child := range errorRows {
		if bucket, ok := byBucket[dimensionBucketKey(child.Granularity, child.Timestamp)]; ok {
			if bucket.ErrorsByType == nil {
				bucket.ErrorsByType = make(map[string]int64)
			}
			bucket.ErrorsByType[child.ErrorType] = child.Count
		}
	}
	return nil
}

// dimensionBucketKey groups dimension children under their base bucket.
func dimensionBucketKey(granularity string, timestamp time.Time) string {
	return granularity + "\x00" + timestamp.UTC().Format(time.RFC3339)
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

// persistMetricBucketDimensions fans out dimension child rows for one bucket.
// Children replace wholesale per bucket: the delete plus inserts run in the
// caller transaction on the transaction handle only.
func (s *Service) persistMetricBucketDimensions(ctx context.Context, transaction *gorm.DB, bucket *MetricBucket, granularity string) error {
	if err := s.persistMetricBucketProviders(ctx, transaction, bucket, granularity); err != nil {
		return err
	}
	if err := s.persistMetricBucketModels(ctx, transaction, bucket, granularity); err != nil {
		return err
	}
	if err := s.persistMetricBucketTokens(ctx, transaction, bucket, granularity); err != nil {
		return err
	}
	return s.persistMetricBucketErrorTypes(ctx, transaction, bucket, granularity)
}

// persistMetricBucketProviders writes metric_bucket_providers rows for both
// dimensions (provider_id and provider_type). Duration sums stay out: the
// table carries requests, errors, tokens, and peaks only.
func (s *Service) persistMetricBucketProviders(ctx context.Context, transaction *gorm.DB, bucket *MetricBucket, granularity string) error {
	timestamp := bucket.Timestamp.UTC()
	if _, err := gorm.G[record.MetricBucketProvider](transaction,
		clause.Eq{Column: "granularity", Value: granularity},
		clause.Eq{Column: "timestamp", Value: timestamp}).Delete(ctx); err != nil {
		return db.Translate(err)
	}
	rows := make([]record.MetricBucketProvider, 0, len(bucket.ByProviderID)+len(bucket.ByProviderType))
	for key, providerMetrics := range bucket.ByProviderID {
		rows = append(rows, record.MetricBucketProvider{
			Granularity: granularity, Timestamp: timestamp, Dimension: "provider_id", Key: key,
			Requests: providerMetrics.Requests, Errors: providerMetrics.Errors,
			TokensInput: providerMetrics.TokensInput, TokensOutput: providerMetrics.TokensOutput,
			PeakRequests: providerMetrics.PeakRequests, PeakInputTokens: providerMetrics.PeakInputTokens, PeakOutputTokens: providerMetrics.PeakOutputTokens,
		})
	}
	for key, providerMetrics := range bucket.ByProviderType {
		rows = append(rows, record.MetricBucketProvider{
			Granularity: granularity, Timestamp: timestamp, Dimension: "provider_type", Key: key,
			Requests: providerMetrics.Requests, Errors: providerMetrics.Errors,
			TokensInput: providerMetrics.TokensInput, TokensOutput: providerMetrics.TokensOutput,
			PeakRequests: providerMetrics.PeakRequests, PeakInputTokens: providerMetrics.PeakInputTokens, PeakOutputTokens: providerMetrics.PeakOutputTokens,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	if err := gorm.G[record.MetricBucketProvider](transaction).CreateInBatches(ctx, &rows, maxFlushBatchSize); err != nil {
		return db.Translate(err)
	}
	return nil
}

// persistMetricBucketModels writes metric_bucket_models rows.
func (s *Service) persistMetricBucketModels(ctx context.Context, transaction *gorm.DB, bucket *MetricBucket, granularity string) error {
	timestamp := bucket.Timestamp.UTC()
	if _, err := gorm.G[record.MetricBucketModel](transaction,
		clause.Eq{Column: "granularity", Value: granularity},
		clause.Eq{Column: "timestamp", Value: timestamp}).Delete(ctx); err != nil {
		return db.Translate(err)
	}
	rows := make([]record.MetricBucketModel, 0, len(bucket.ByModel))
	for key, modelMetrics := range bucket.ByModel {
		rows = append(rows, record.MetricBucketModel{
			Granularity: granularity, Timestamp: timestamp, ModelID: key,
			Requests: modelMetrics.Requests, Errors: modelMetrics.Errors,
			TokensInput: modelMetrics.TokensInput, TokensOutput: modelMetrics.TokensOutput,
			PeakRequests: modelMetrics.PeakRequests, PeakInputTokens: modelMetrics.PeakInputTokens, PeakOutputTokens: modelMetrics.PeakOutputTokens,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	if err := gorm.G[record.MetricBucketModel](transaction).CreateInBatches(ctx, &rows, maxFlushBatchSize); err != nil {
		return db.Translate(err)
	}
	return nil
}

// persistMetricBucketTokens writes metric_bucket_tokens rows.
func (s *Service) persistMetricBucketTokens(ctx context.Context, transaction *gorm.DB, bucket *MetricBucket, granularity string) error {
	timestamp := bucket.Timestamp.UTC()
	if _, err := gorm.G[record.MetricBucketToken](transaction,
		clause.Eq{Column: "granularity", Value: granularity},
		clause.Eq{Column: "timestamp", Value: timestamp}).Delete(ctx); err != nil {
		return db.Translate(err)
	}
	rows := make([]record.MetricBucketToken, 0, len(bucket.ByTokenID))
	for key, tokenMetrics := range bucket.ByTokenID {
		var lastUsed *time.Time
		if tokenMetrics.LastUsed != nil {
			copied := tokenMetrics.LastUsed.UTC()
			lastUsed = &copied
		}
		rows = append(rows, record.MetricBucketToken{
			Granularity: granularity, Timestamp: timestamp, TokenID: key,
			Requests: tokenMetrics.Requests, Errors: tokenMetrics.Errors,
			TokensInput: tokenMetrics.TokensInput, TokensOutput: tokenMetrics.TokensOutput,
			PeakRequests: tokenMetrics.PeakRequests, PeakInputTokens: tokenMetrics.PeakInputTokens, PeakOutputTokens: tokenMetrics.PeakOutputTokens,
			LastUsed: lastUsed,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	if err := gorm.G[record.MetricBucketToken](transaction).CreateInBatches(ctx, &rows, maxFlushBatchSize); err != nil {
		return db.Translate(err)
	}
	return nil
}

// persistMetricBucketErrorTypes writes metric_bucket_error_types rows.
func (s *Service) persistMetricBucketErrorTypes(ctx context.Context, transaction *gorm.DB, bucket *MetricBucket, granularity string) error {
	timestamp := bucket.Timestamp.UTC()
	if _, err := gorm.G[record.MetricBucketErrorType](transaction,
		clause.Eq{Column: "granularity", Value: granularity},
		clause.Eq{Column: "timestamp", Value: timestamp}).Delete(ctx); err != nil {
		return db.Translate(err)
	}
	rows := make([]record.MetricBucketErrorType, 0, len(bucket.ErrorsByType))
	for errorType, count := range bucket.ErrorsByType {
		rows = append(rows, record.MetricBucketErrorType{
			Granularity: granularity, Timestamp: timestamp, ErrorType: errorType, Count: count,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	if err := gorm.G[record.MetricBucketErrorType](transaction).CreateInBatches(ctx, &rows, maxFlushBatchSize); err != nil {
		return db.Translate(err)
	}
	return nil
}
