// SQLiteService persists batches in SQLite: one header row in
// message_batches plus ordered child rows in message_batch_entries and
// message_batch_results. Entry params and result payloads stay verbatim
// JSON. Every write runs inside one transaction; child rows carry the
// request order in their position column.
package batches

import (
	"context"
	"errors"
	"log/slog"
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

// Terminal batch states: no further transitions leave them.
const (
	StatusInProgress = "in_progress"
	StatusEnded      = "ended"
	StatusCanceled   = "canceled"
)

// SQLiteService owns MessageBatch rows and their ordered children.
type SQLiteService struct {
	database *db.Database
	logger   *slog.Logger
}

// NewSQLiteService constructs a batches SQLiteService. Construction
// performs no I/O.
func NewSQLiteService(database *db.Database) *SQLiteService {
	return &SQLiteService{database: database}
}

// SetLogger wires structured logging (called once from server.New).
func (s *SQLiteService) SetLogger(l *slog.Logger) { s.logger = l }

func (s *SQLiteService) log() *slog.Logger {
	if s == nil || s.logger == nil {
		return slog.Default()
	}
	return s.logger
}

// isTerminalBatchStatus reports whether a batch status closes the batch.
func isTerminalBatchStatus(status string) bool {
	return status == StatusEnded || status == StatusCanceled
}

// Create persists a new batch: header plus ordered children in one
// transaction.
func (s *SQLiteService) Create(ctx context.Context, batch *models.BatchRecord) error {
	if batch == nil {
		s.log().Debug("Message batch create validation failed", "reason", "batch required")
		return errors.New("message batch is required")
	}
	if batch.ID == "" {
		s.log().Debug("Message batch create validation failed", "reason", "batch id required")
		return errors.New("message batch id is required")
	}
	now := time.Now()
	batch.CreatedAt = now
	batch.UpdatedAt = now
	err := s.database.Cache.Transaction(func(transaction *gorm.DB) error {
		headers := repository.NewGormRepository[record.MessageBatch](transaction, "id")
		entries := repository.NewGormRepository[record.MessageBatchEntry](transaction, "batch_id", "position")
		results := repository.NewGormRepository[record.MessageBatchResult](transaction, "batch_id", "position")
		if err := headers.Upsert(ctx, toBatchHeader(batch)); err != nil {
			s.log().Error("Message batch create failed", "batch_id", batch.ID, "event", "header", "error", err)
			return err
		}
		if err := entries.Upsert(ctx, toBatchEntries(batch)...); err != nil {
			s.log().Error("Message batch create failed", "batch_id", batch.ID, "event", "entries", "error", err)
			return err
		}
		if err := results.Upsert(ctx, toBatchResults(batch)...); err != nil {
			s.log().Error("Message batch create failed", "batch_id", batch.ID, "event", "results", "error", err)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.log().Info("Message batch created", "batch_id", batch.ID, "status", batch.Status, "count", len(batch.Entries), "model", string(batch.Model))
	return nil
}

// Get returns the batch by ID with children in position order.
func (s *SQLiteService) Get(ctx context.Context, id string) (*models.BatchRecord, error) {
	headers := repository.NewGormRepository[record.MessageBatch](s.database.Cache, "id")
	entries := repository.NewGormRepository[record.MessageBatchEntry](s.database.Cache, "batch_id", "position")
	results := repository.NewGormRepository[record.MessageBatchResult](s.database.Cache, "batch_id", "position")
	header, err := headers.Get(ctx, clause.Eq{Column: "id", Value: id})
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.log().Debug("Message batch not found", "batch_id", id)
			return nil, apierrors.NewNotFoundError("message batch", id)
		}
		s.log().Error("Message batch fetch failed", "batch_id", id, "event", "header", "error", err)
		return nil, err
	}
	entryRows, err := entries.List(ctx, clause.Eq{Column: "batch_id", Value: id})
	if err != nil {
		s.log().Error("Message batch fetch failed", "batch_id", id, "event", "entries", "error", err)
		return nil, err
	}
	resultRows, err := results.List(ctx, clause.Eq{Column: "batch_id", Value: id})
	if err != nil {
		s.log().Error("Message batch fetch failed", "batch_id", id, "event", "results", "error", err)
		return nil, err
	}
	assembled := assembleBatchRecord(header, entryRows, resultRows)
	s.log().Debug("Message batch fetched", "batch_id", id, "status", assembled.Status, "count", len(assembled.Entries))
	return assembled, nil
}

// Update rewrites a batch: header plus replaced children in one
// transaction. Missing rows return not found.
func (s *SQLiteService) Update(ctx context.Context, batch *models.BatchRecord) error {
	if batch == nil || batch.ID == "" {
		s.log().Debug("Message batch update validation failed", "reason", "batch id required")
		return errors.New("message batch id is required")
	}
	var previousStatus string
	err := s.database.Cache.Transaction(func(transaction *gorm.DB) error {
		headers := repository.NewGormRepository[record.MessageBatch](transaction, "id")
		entries := repository.NewGormRepository[record.MessageBatchEntry](transaction, "batch_id", "position")
		results := repository.NewGormRepository[record.MessageBatchResult](transaction, "batch_id", "position")
		existing, err := headers.Get(ctx, clause.Eq{Column: "id", Value: batch.ID})
		if err != nil {
			if errors.Is(err, apierrors.ErrNotFound) {
				s.log().Debug("Message batch not found", "batch_id", batch.ID)
				return apierrors.NewNotFoundError("message batch", batch.ID)
			}
			s.log().Error("Message batch update failed", "batch_id", batch.ID, "event", "header_fetch", "error", err)
			return err
		}
		previousStatus = existing.Status
		batch.CreatedAt = existing.CreatedAt
		batch.UpdatedAt = time.Now()
		if _, err := entries.Delete(ctx, clause.Eq{Column: "batch_id", Value: batch.ID}); err != nil {
			s.log().Error("Message batch update failed", "batch_id", batch.ID, "event", "entries_delete", "error", err)
			return err
		}
		if _, err := results.Delete(ctx, clause.Eq{Column: "batch_id", Value: batch.ID}); err != nil {
			s.log().Error("Message batch update failed", "batch_id", batch.ID, "event", "results_delete", "error", err)
			return err
		}
		if err := headers.Upsert(ctx, toBatchHeader(batch)); err != nil {
			s.log().Error("Message batch update failed", "batch_id", batch.ID, "event", "header", "error", err)
			return err
		}
		if err := entries.Upsert(ctx, toBatchEntries(batch)...); err != nil {
			s.log().Error("Message batch update failed", "batch_id", batch.ID, "event", "entries", "error", err)
			return err
		}
		if err := results.Upsert(ctx, toBatchResults(batch)...); err != nil {
			s.log().Error("Message batch update failed", "batch_id", batch.ID, "event", "results", "error", err)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if isTerminalBatchStatus(previousStatus) && !isTerminalBatchStatus(batch.Status) {
		s.log().Warn("Message batch reopened", "batch_id", batch.ID, "status", batch.Status)
	}
	s.log().Info("Message batch updated", "batch_id", batch.ID, "status", batch.Status, "count", len(batch.Entries))
	return nil
}

// Delete removes a batch row; the foreign key cascade removes its
// children. Missing rows are not errors.
func (s *SQLiteService) Delete(ctx context.Context, id string) error {
	err := s.database.Cache.Transaction(func(transaction *gorm.DB) error {
		headers := repository.NewGormRepository[record.MessageBatch](transaction, "id")
		if _, err := headers.Delete(ctx, clause.Eq{Column: "id", Value: id}); err != nil {
			s.log().Error("Message batch delete failed", "batch_id", id, "error", err)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.log().Info("Message batch deleted", "batch_id", id)
	return nil
}

// List returns all batches with children, oldest first (ID tiebreaks
// clock ties).
func (s *SQLiteService) List(ctx context.Context) ([]*models.BatchRecord, error) {
	headers := repository.NewGormRepository[record.MessageBatch](s.database.Cache, "id")
	entries := repository.NewGormRepository[record.MessageBatchEntry](s.database.Cache, "batch_id", "position")
	results := repository.NewGormRepository[record.MessageBatchResult](s.database.Cache, "batch_id", "position")
	headerRows, err := headers.List(ctx)
	if err != nil {
		s.log().Error("Message batches list failed", "event", "header", "error", err)
		return nil, err
	}
	entryRows, err := entries.List(ctx)
	if err != nil {
		s.log().Error("Message batches list failed", "event", "entries", "error", err)
		return nil, err
	}
	resultRows, err := results.List(ctx)
	if err != nil {
		s.log().Error("Message batches list failed", "event", "results", "error", err)
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
	s.log().Debug("Message batches listed", "count", len(batches))
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
