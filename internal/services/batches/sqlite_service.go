// SQLiteService persists batches in SQLite: one header row in
// message_batches plus ordered child rows in message_batch_entries and
// message_batch_results. Entry params and result payloads stay verbatim
// JSON. Every write runs inside one transaction; child rows carry the
// request order in their position column. The bbolt Service stays until
// Wave 10 cutover.
package batches

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SQLiteService owns MessageBatch rows and their ordered children.
type SQLiteService struct {
	database *db.Database
}

// NewSQLiteService constructs a batches SQLiteService. Construction
// performs no I/O.
func NewSQLiteService(database *db.Database) *SQLiteService {
	return &SQLiteService{database: database}
}

// Create persists a new batch: header plus ordered children in one
// transaction.
func (s *SQLiteService) Create(batch *models.BatchRecord) error {
	if batch == nil {
		return errors.New("message batch is required")
	}
	if batch.ID == "" {
		return errors.New("message batch id is required")
	}
	now := time.Now()
	batch.CreatedAt = now
	batch.UpdatedAt = now
	return s.database.Handle.Transaction(func(transaction *gorm.DB) error {
		headers := repository.NewGormRepository[record.MessageBatch](transaction, "id")
		entries := repository.NewGormRepository[record.MessageBatchEntry](transaction, "batch_id", "position")
		results := repository.NewGormRepository[record.MessageBatchResult](transaction, "batch_id", "position")
		if err := headers.Upsert(context.Background(), toBatchHeader(batch)); err != nil {
			return err
		}
		if err := entries.Upsert(context.Background(), toBatchEntries(batch)...); err != nil {
			return err
		}
		return results.Upsert(context.Background(), toBatchResults(batch)...)
	})
}

// Get returns the batch by ID with children in position order.
func (s *SQLiteService) Get(id string) (*models.BatchRecord, error) {
	ctx := context.Background()
	headers := repository.NewGormRepository[record.MessageBatch](s.database.Handle, "id")
	entries := repository.NewGormRepository[record.MessageBatchEntry](s.database.Handle, "batch_id", "position")
	results := repository.NewGormRepository[record.MessageBatchResult](s.database.Handle, "batch_id", "position")
	header, err := headers.Get(ctx, clause.Eq{Column: "id", Value: id})
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, apierrors.NewNotFoundError("message batch", id)
		}
		return nil, err
	}
	entryRows, err := entries.List(ctx, clause.Eq{Column: "batch_id", Value: id})
	if err != nil {
		return nil, err
	}
	resultRows, err := results.List(ctx, clause.Eq{Column: "batch_id", Value: id})
	if err != nil {
		return nil, err
	}
	return assembleBatchRecord(header, entryRows, resultRows), nil
}

// Update rewrites a batch: header plus replaced children in one
// transaction. Missing rows return not found.
func (s *SQLiteService) Update(batch *models.BatchRecord) error {
	if batch == nil || batch.ID == "" {
		return errors.New("message batch id is required")
	}
	return s.database.Handle.Transaction(func(transaction *gorm.DB) error {
		ctx := context.Background()
		headers := repository.NewGormRepository[record.MessageBatch](transaction, "id")
		entries := repository.NewGormRepository[record.MessageBatchEntry](transaction, "batch_id", "position")
		results := repository.NewGormRepository[record.MessageBatchResult](transaction, "batch_id", "position")
		existing, err := headers.Get(ctx, clause.Eq{Column: "id", Value: batch.ID})
		if err != nil {
			if errors.Is(err, apierrors.ErrNotFound) {
				return apierrors.NewNotFoundError("message batch", batch.ID)
			}
			return err
		}
		batch.CreatedAt = existing.CreatedAt
		batch.UpdatedAt = time.Now()
		if _, err := entries.Delete(ctx, clause.Eq{Column: "batch_id", Value: batch.ID}); err != nil {
			return err
		}
		if _, err := results.Delete(ctx, clause.Eq{Column: "batch_id", Value: batch.ID}); err != nil {
			return err
		}
		if err := headers.Upsert(ctx, toBatchHeader(batch)); err != nil {
			return err
		}
		if err := entries.Upsert(ctx, toBatchEntries(batch)...); err != nil {
			return err
		}
		return results.Upsert(ctx, toBatchResults(batch)...)
	})
}

// Delete removes a batch row; the foreign key cascade removes its
// children. Missing rows are not errors.
func (s *SQLiteService) Delete(id string) error {
	ctx := context.Background()
	return s.database.Handle.Transaction(func(transaction *gorm.DB) error {
		headers := repository.NewGormRepository[record.MessageBatch](transaction, "id")
		_, err := headers.Delete(ctx, clause.Eq{Column: "id", Value: id})
		return err
	})
}

// List returns all batches with children, oldest first (ID tiebreaks
// clock ties).
func (s *SQLiteService) List() ([]*models.BatchRecord, error) {
	ctx := context.Background()
	headers := repository.NewGormRepository[record.MessageBatch](s.database.Handle, "id")
	entries := repository.NewGormRepository[record.MessageBatchEntry](s.database.Handle, "batch_id", "position")
	results := repository.NewGormRepository[record.MessageBatchResult](s.database.Handle, "batch_id", "position")
	headerRows, err := headers.List(ctx)
	if err != nil {
		return nil, err
	}
	entryRows, err := entries.List(ctx)
	if err != nil {
		return nil, err
	}
	resultRows, err := results.List(ctx)
	if err != nil {
		return nil, err
	}
	entriesByBatch := groupBatchEntries(entryRows)
	resultsByBatch := groupBatchResults(resultRows)
	batches := make([]*models.BatchRecord, 0, len(headerRows))
	for _, header := range headerRows {
		batches = append(batches, assembleBatchRecord(header, entriesByBatch[header.ID], resultsByBatch[header.ID]))
	}
	sort.Slice(batches, func(i, j int) bool {
		if batches[i].CreatedAt.Equal(batches[j].CreatedAt) {
			return batches[i].ID < batches[j].ID
		}
		return batches[i].CreatedAt.Before(batches[j].CreatedAt)
	})
	return batches, nil
}

// toBatchHeader maps the batch envelope to its header row.
func toBatchHeader(batch *models.BatchRecord) record.MessageBatch {
	return record.MessageBatch{
		ID:        batch.ID,
		Model:     string(batch.Model),
		Status:    batch.Status,
		CreatedAt: batch.CreatedAt,
		UpdatedAt: batch.UpdatedAt,
		EndedAt:   batch.EndedAt,
	}
}

// toBatchEntries maps batch entries to ordered child rows. Payload bytes
// stay verbatim; nil payloads become empty blobs for the NOT NULL column.
func toBatchEntries(batch *models.BatchRecord) []record.MessageBatchEntry {
	rows := make([]record.MessageBatchEntry, 0, len(batch.Entries))
	for position, entry := range batch.Entries {
		rows = append(rows, record.MessageBatchEntry{
			BatchID:  batch.ID,
			Position: position,
			CustomID: entry.CustomID,
			Params:   nonNullJSON(entry.Params),
		})
	}
	return rows
}

// toBatchResults maps batch results to ordered child rows. Payload bytes
// stay verbatim; nil payloads become empty blobs for the NOT NULL column.
func toBatchResults(batch *models.BatchRecord) []record.MessageBatchResult {
	rows := make([]record.MessageBatchResult, 0, len(batch.Results))
	for position, result := range batch.Results {
		rows = append(rows, record.MessageBatchResult{
			BatchID:  batch.ID,
			Position: position,
			CustomID: result.CustomID,
			Result:   nonNullJSON(result.Result),
		})
	}
	return rows
}

// nonNullJSON keeps JSON payloads verbatim while satisfying NOT NULL.
func nonNullJSON(payload []byte) []byte {
	if payload == nil {
		return []byte{}
	}
	return payload
}

// assembleBatchRecord rebuilds the envelope from a header and its child
// rows in position order.
func assembleBatchRecord(
	header record.MessageBatch,
	entryRows []record.MessageBatchEntry,
	resultRows []record.MessageBatchResult,
) *models.BatchRecord {
	orderedEntries := append([]record.MessageBatchEntry(nil), entryRows...)
	orderedResults := append([]record.MessageBatchResult(nil), resultRows...)
	sort.Slice(orderedEntries, func(i, j int) bool { return orderedEntries[i].Position < orderedEntries[j].Position })
	sort.Slice(orderedResults, func(i, j int) bool { return orderedResults[i].Position < orderedResults[j].Position })
	batch := &models.BatchRecord{
		ID:        header.ID,
		Model:     models.ModelId(header.Model),
		Status:    header.Status,
		CreatedAt: header.CreatedAt,
		UpdatedAt: header.UpdatedAt,
		EndedAt:   header.EndedAt,
	}
	for _, row := range orderedEntries {
		batch.Entries = append(batch.Entries, models.BatchEntry{CustomID: row.CustomID, Params: row.Params})
	}
	for _, row := range orderedResults {
		batch.Results = append(batch.Results, models.BatchResultLine{CustomID: row.CustomID, Result: row.Result})
	}
	return batch
}

// groupBatchEntries indexes entry rows by batch ID.
func groupBatchEntries(rows []record.MessageBatchEntry) map[string][]record.MessageBatchEntry {
	grouped := map[string][]record.MessageBatchEntry{}
	for _, row := range rows {
		grouped[row.BatchID] = append(grouped[row.BatchID], row)
	}
	return grouped
}

// groupBatchResults indexes result rows by batch ID.
func groupBatchResults(rows []record.MessageBatchResult) map[string][]record.MessageBatchResult {
	grouped := map[string][]record.MessageBatchResult{}
	for _, row := range rows {
		grouped[row.BatchID] = append(grouped[row.BatchID], row)
	}
	return grouped
}
