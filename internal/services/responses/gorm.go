package responses

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormService owns response records in the response_records table.
// Payload and input stay opaque JSON, parent references stay soft.
type GormService struct {
	handle *gorm.DB
	store  *repository.GormRepository[record.ResponseRecord]
}

// NewGormService constructs a GormService on the base handle. Construction
// performs no I/O. The handle is stored; each call derives its chain from
// it, so reused chains never leak conditions across calls.
func NewGormService(handle *gorm.DB) *GormService {
	return &GormService{
		handle: handle,
		store:  repository.NewGormRepository[record.ResponseRecord](handle, "id"),
	}
}

// toStoredRecord maps the wire record onto the persistence row. An empty
// parent ID becomes NULL, and absent bodies become empty blobs: the payload
// and input columns reject NULL, and callers treat zero length as absent.
func toStoredRecord(current *models.ResponseRecord) record.ResponseRecord {
	row := record.ResponseRecord{
		ID:        current.ID,
		Kind:      string(current.Kind),
		Model:     string(current.Model),
		Status:    current.Status,
		RefID:     current.RefID,
		CreatedAt: current.CreatedAt,
		UpdatedAt: current.UpdatedAt,
	}
	if current.ParentID != "" {
		parent := current.ParentID
		row.ParentID = &parent
	}
	row.Payload = append([]byte{}, []byte(current.Payload)...)
	row.Input = append([]byte{}, []byte(current.Input)...)
	return row
}

// fromStoredRecord maps the persistence row back onto the wire record.
// NULL parents and empty bodies return as absent values.
func fromStoredRecord(row record.ResponseRecord) *models.ResponseRecord {
	current := &models.ResponseRecord{
		ID:        row.ID,
		Kind:      models.ResponseRecordKind(row.Kind),
		Model:     models.ModelId(row.Model),
		Status:    row.Status,
		RefID:     row.RefID,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
	if row.ParentID != nil {
		current.ParentID = *row.ParentID
	}
	if len(row.Payload) > 0 {
		current.Payload = json.RawMessage(append([]byte{}, row.Payload...))
	}
	if len(row.Input) > 0 {
		current.Input = json.RawMessage(append([]byte{}, row.Input...))
	}
	return current
}

// Create persists a new record: one public method, one transaction.
// Existing IDs are overwritten, matching the previous Put behavior.
func (s *GormService) Create(ctx context.Context, current *models.ResponseRecord) error {
	if current == nil {
		return errors.New("compat record is required")
	}
	if current.ID == "" {
		return errors.New("compat record id is required")
	}
	now := time.Now().UTC()
	current.CreatedAt = now
	current.UpdatedAt = now
	row := toStoredRecord(current)
	return s.handle.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return repository.NewGormRepository[record.ResponseRecord](tx, "id").Upsert(ctx, row)
	})
}

// Get returns the record by ID, optionally asserting its kind.
func (s *GormService) Get(ctx context.Context, id string, kinds ...models.ResponseRecordKind) (*models.ResponseRecord, error) {
	row, err := s.store.Get(ctx, clause.Eq{Column: clause.Column{Name: "id"}, Value: id})
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, apierrors.NewNotFoundError("compat record", id)
		}
		return nil, err
	}
	current := fromStoredRecord(row)
	for _, kind := range kinds {
		if current.Kind == kind {
			return current, nil
		}
	}
	if len(kinds) > 0 {
		return nil, apierrors.NewNotFoundError("compat record", id)
	}
	return current, nil
}

// Update rewrites a record in place: one public method, one transaction.
// Missing rows return the not-found sentinel.
func (s *GormService) Update(ctx context.Context, current *models.ResponseRecord) error {
	if current == nil || current.ID == "" {
		return errors.New("compat record id is required")
	}
	return s.handle.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		store := repository.NewGormRepository[record.ResponseRecord](tx, "id")
		existing, err := store.Get(ctx, clause.Eq{Column: clause.Column{Name: "id"}, Value: current.ID})
		if err != nil {
			return err
		}
		row := toStoredRecord(current)
		row.CreatedAt = existing.CreatedAt
		row.UpdatedAt = time.Now().UTC()
		current.CreatedAt = existing.CreatedAt
		current.UpdatedAt = row.UpdatedAt
		return store.Upsert(ctx, row)
	})
}

// Delete removes a record. Missing rows are not errors.
func (s *GormService) Delete(ctx context.Context, id string) error {
	return s.handle.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		store := repository.NewGormRepository[record.ResponseRecord](tx, "id")
		_, err := store.Delete(ctx, clause.Eq{Column: clause.Column{Name: "id"}, Value: id})
		return err
	})
}

// List returns records of the given kinds (empty kinds = all), oldest first.
// ParentID filters thread children and run bindings when non-empty.
func (s *GormService) List(ctx context.Context, kinds []models.ResponseRecordKind, parentID string) ([]*models.ResponseRecord, error) {
	conditions := make([]clause.Expression, 0, 3)
	if len(kinds) > 0 {
		values := make([]any, 0, len(kinds))
		for _, kind := range kinds {
			values = append(values, string(kind))
		}
		conditions = append(conditions, clause.IN{Column: clause.Column{Name: "kind"}, Values: values})
	}
	if parentID != "" {
		conditions = append(conditions, clause.Eq{Column: clause.Column{Name: "parent_id"}, Value: parentID})
	}
	conditions = append(conditions, clause.OrderBy{Columns: []clause.OrderByColumn{
		{Column: clause.Column{Name: "created_at"}},
		{Column: clause.Column{Name: "id"}},
	}})
	rows, err := s.store.List(ctx, conditions...)
	if err != nil {
		return nil, err
	}
	records := make([]*models.ResponseRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, fromStoredRecord(row))
	}
	return records, nil
}
