package record

import (
	"time"
)

// VideoJob persists one router-side video generation job: the local ID
// handed to the client plus the upstream job it polls. The provider column
// is a soft history reference, deliberately without a foreign key.
type VideoJob struct {
	ID            string    `gorm:"primaryKey"`
	Model         string    `gorm:"not null"`
	BackendModel  string    `gorm:"not null"`
	ProviderID    string    `gorm:"not null"`
	TypeKey       string    `gorm:"not null"`
	UpstreamJobID string    `gorm:"not null"`
	Status        string    `gorm:"not null;index"`
	CreatedAt     time.Time `gorm:"not null"`
	UpdatedAt     time.Time `gorm:"not null"`
}

// TableName pins the video jobs table name.
func (VideoJob) TableName() string {
	return "video_jobs"
}

// ResponseRecord is one async compat object: response, conversation,
// assistant, thread, message, or run. Payload and Input stay raw JSON
// because the bodies are client-owned. ParentID is a soft self-reference.
type ResponseRecord struct {
	ID        string    `gorm:"primaryKey"`
	Kind      string    `gorm:"not null;index"`
	Model     string    `gorm:"not null"`
	Status    string    `gorm:"not null"`
	ParentID  *string   `gorm:"index"`
	RefID     string    `gorm:"not null"`
	Payload   []byte    `gorm:"not null"`
	Input     []byte    `gorm:"not null"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

// TableName pins the response records table name.
func (ResponseRecord) TableName() string {
	return "response_records"
}

// MessageBatch is one Anthropic message batch header.
type MessageBatch struct {
	ID        string     `gorm:"primaryKey"`
	Model     string     `gorm:"not null"`
	Status    string     `gorm:"not null;index"`
	CreatedAt time.Time  `gorm:"not null"`
	UpdatedAt time.Time  `gorm:"not null"`
	EndedAt   *time.Time `gorm:""`
}

// TableName pins the message batches table name.
func (MessageBatch) TableName() string {
	return "message_batches"
}

// MessageBatchEntry is one ordered request inside a batch.
type MessageBatchEntry struct {
	BatchID  string       `gorm:"primaryKey"`
	Batch    MessageBatch `gorm:"foreignKey:BatchID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Position int          `gorm:"primaryKey"`
	CustomID string       `gorm:"not null"`
	Params   []byte       `gorm:"not null"`
}

// TableName pins the message batch entries table name.
func (MessageBatchEntry) TableName() string {
	return "message_batch_entries"
}

// MessageBatchResult is one ordered result inside a batch.
type MessageBatchResult struct {
	BatchID  string       `gorm:"primaryKey"`
	Batch    MessageBatch `gorm:"foreignKey:BatchID;references:ID;belongsTo;constraint:OnDelete:CASCADE"`
	Position int          `gorm:"primaryKey"`
	CustomID string       `gorm:"not null"`
	Result   []byte       `gorm:"not null"`
}

// TableName pins the message batch results table name.
func (MessageBatchResult) TableName() string {
	return "message_batch_results"
}
