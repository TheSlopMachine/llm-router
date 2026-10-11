package metrics

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
)

// persistBucket saves a bucket with its dimensions to SQLite in one
// transaction.
func (s *Service) persistBucket(bucket *MetricBucket, granularity string) error {
	if bucket == nil {
		return nil
	}
	return s.persistBucketSQLite(context.Background(), bucket, granularity)
}

// loadBucket loads a single bucket with its dimensions from SQLite.
func (s *Service) loadBucket(timestamp time.Time, granularity string) (*MetricBucket, error) {
	bucket, err := s.loadBucketSQLite(context.Background(), timestamp, granularity)
	if err == nil {
		return bucket, nil
	}
	if !errors.Is(err, apierrors.ErrNotFound) {
		return nil, err
	}
	return nil, apierrors.NewNotFoundError("metric bucket", timestamp.UTC().Format(time.RFC3339)+"_"+granularity)
}

// loadBucketsInRange loads all buckets within a time range from SQLite with
// dimensions attached.
func (s *Service) loadBucketsInRange(start, end time.Time) ([]*MetricBucket, error) {
	return s.loadBucketsInRangeSQLite(context.Background(), start, end)
}

// deleteBucketsOlderThan deletes all buckets older than the cutoff time
// from SQLite. Dimension children cascade from the base deletes.
func (s *Service) deleteBucketsOlderThan(cutoff time.Time) error {
	removed, err := s.deleteBucketsOlderThanSQLite(context.Background(), cutoff)
	if err != nil {
		return err
	}
	if removed > 0 {
		s.logger.Info("deleted old buckets", "count", removed)
	}
	return nil
}

// loadTokenUsageFromDB aggregates all-time token usage from the SQLite
// token dimension rows.
func (s *Service) loadTokenUsageFromDB() (map[string]*TokenUsageInfo, error) {
	usage := make(map[string]*TokenUsageInfo)
	if s.bucketRepository == nil {
		return usage, nil
	}
	rows, err := s.listTokenDimensionRows(context.Background())
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if usage[row.TokenID] == nil {
			usage[row.TokenID] = &TokenUsageInfo{}
		}
		usage[row.TokenID].Requests += row.Requests
		if row.LastUsed != nil {
			if usage[row.TokenID].LastUsed == nil || row.LastUsed.After(*usage[row.TokenID].LastUsed) {
				copied := *row.LastUsed
				usage[row.TokenID].LastUsed = &copied
			}
		}
	}
	return usage, nil
}

// listTokenDimensionRows returns every token dimension row for usage
// aggregation. Callers fold requests and latest use in Go.
func (s *Service) listTokenDimensionRows(ctx context.Context) ([]record.MetricBucketToken, error) {
	if s.sqliteDatabase == nil {
		return nil, nil
	}
	rows, err := gorm.G[record.MetricBucketToken](s.sqliteDatabase).Find(ctx)
	if err != nil {
		return nil, db.Translate(err)
	}
	return rows, nil
}

// listModelDimensionRows returns every model dimension row for distinct
// model listing. Callers deduplicate in Go.
func (s *Service) listModelDimensionRows(ctx context.Context) ([]record.MetricBucketModel, error) {
	if s.sqliteDatabase == nil {
		return nil, nil
	}
	rows, err := gorm.G[record.MetricBucketModel](s.sqliteDatabase).Find(ctx)
	if err != nil {
		return nil, db.Translate(err)
	}
	return rows, nil
}

// loadDistinctModelsFromDB returns every model ID present in the SQLite
// model dimension rows.
func (s *Service) loadDistinctModelsFromDB() ([]string, error) {
	rows, err := s.listModelDimensionRows(context.Background())
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	models := []string{}
	for _, row := range rows {
		if !seen[row.ModelID] {
			seen[row.ModelID] = true
			models = append(models, row.ModelID)
		}
	}
	return models, nil
}
