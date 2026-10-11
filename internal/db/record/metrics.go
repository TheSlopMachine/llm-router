package record

import (
	"time"
)

// MetricBucket is one aggregated time window. Dimension child tables share
// this header through a composite foreign key. Usage history outlives
// providers, models, and tokens, so children carry no foreign keys to
// those rows: deleting the parent bucket cascades, deleting a provider
// never erases history.
type MetricBucket struct {
	Granularity             string    `gorm:"primaryKey"`
	Timestamp               time.Time `gorm:"primaryKey;index:idx_metric_buckets_timestamp"`
	TotalRequests           int64     `gorm:"not null"`
	TotalErrors             int64     `gorm:"not null"`
	TokensInput             int64     `gorm:"not null"`
	TokensOutput            int64     `gorm:"not null"`
	PeakRequests            int64     `gorm:"not null"`
	PeakInputTokens         int64     `gorm:"not null"`
	PeakOutputTokens        int64     `gorm:"not null"`
	DurationSumMicroseconds int64     `gorm:"not null"`
	DurationCount           int64     `gorm:"not null"`
}

// TableName pins the metric buckets table name.
func (MetricBucket) TableName() string {
	return "metric_buckets"
}

// MetricBucketProvider is one provider dimension row. Dimension reads
// provider_id or provider_type; both share this table.
type MetricBucketProvider struct {
	Granularity      string       `gorm:"primaryKey"`
	Timestamp        time.Time    `gorm:"primaryKey"`
	Bucket           MetricBucket `gorm:"foreignKey:Granularity,Timestamp;references:Granularity,Timestamp;belongsTo;constraint:OnDelete:CASCADE"`
	Dimension        string       `gorm:"primaryKey;check:dimension IN ('provider_id','provider_type')"`
	Key              string       `gorm:"primaryKey"`
	Requests         int64        `gorm:"not null"`
	Errors           int64        `gorm:"not null"`
	TokensInput      int64        `gorm:"not null"`
	TokensOutput     int64        `gorm:"not null"`
	PeakRequests     int64        `gorm:"not null"`
	PeakInputTokens  int64        `gorm:"not null"`
	PeakOutputTokens int64        `gorm:"not null"`
}

// TableName pins the metric bucket providers table name.
func (MetricBucketProvider) TableName() string {
	return "metric_bucket_providers"
}

// MetricBucketModel is one model dimension row.
type MetricBucketModel struct {
	Granularity      string       `gorm:"primaryKey"`
	Timestamp        time.Time    `gorm:"primaryKey"`
	Bucket           MetricBucket `gorm:"foreignKey:Granularity,Timestamp;references:Granularity,Timestamp;belongsTo;constraint:OnDelete:CASCADE"`
	ModelID          string       `gorm:"primaryKey"`
	Requests         int64        `gorm:"not null"`
	Errors           int64        `gorm:"not null"`
	TokensInput      int64        `gorm:"not null"`
	TokensOutput     int64        `gorm:"not null"`
	PeakRequests     int64        `gorm:"not null"`
	PeakInputTokens  int64        `gorm:"not null"`
	PeakOutputTokens int64        `gorm:"not null"`
}

// TableName pins the metric bucket models table name.
func (MetricBucketModel) TableName() string {
	return "metric_bucket_models"
}

// MetricBucketToken is one token dimension row.
type MetricBucketToken struct {
	Granularity      string       `gorm:"primaryKey"`
	Timestamp        time.Time    `gorm:"primaryKey"`
	Bucket           MetricBucket `gorm:"foreignKey:Granularity,Timestamp;references:Granularity,Timestamp;belongsTo;constraint:OnDelete:CASCADE"`
	TokenID          string       `gorm:"primaryKey"`
	Requests         int64        `gorm:"not null"`
	Errors           int64        `gorm:"not null"`
	TokensInput      int64        `gorm:"not null"`
	TokensOutput     int64        `gorm:"not null"`
	PeakRequests     int64        `gorm:"not null"`
	PeakInputTokens  int64        `gorm:"not null"`
	PeakOutputTokens int64        `gorm:"not null"`
	LastUsed         *time.Time   `gorm:""`
}

// TableName pins the metric bucket tokens table name.
func (MetricBucketToken) TableName() string {
	return "metric_bucket_tokens"
}

// MetricBucketErrorType is one error-type dimension row.
type MetricBucketErrorType struct {
	Granularity string       `gorm:"primaryKey"`
	Timestamp   time.Time    `gorm:"primaryKey"`
	Bucket      MetricBucket `gorm:"foreignKey:Granularity,Timestamp;references:Granularity,Timestamp;belongsTo;constraint:OnDelete:CASCADE"`
	ErrorType   string       `gorm:"primaryKey"`
	Count       int64        `gorm:"not null"`
}

// TableName pins the metric bucket error types table name.
func (MetricBucketErrorType) TableName() string {
	return "metric_bucket_error_types"
}
