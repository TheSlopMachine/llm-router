// Package events owns operator telemetry: append-only event records with
// free-form metadata keys, retention pruning, and filtered queries for
// GET /api/events. All writes run through one service; no other package
// touches the events tables directly.
package events

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Severity levels for event records.
const (
	SeverityDebug = 0
	SeverityInfo  = 1
	SeverityWarn  = 2
	SeverityError = 3
	SeverityFatal = 4
)

// DefaultLimit bounds parameterless queries. MaxLimit caps clients.
const (
	DefaultLimit = 50
	MaxLimit     = 500
)

// pruneInterval paces periodic retention pruning.
const pruneInterval = time.Hour

// KeyFilter is one filter.<key>[=<value>] predicate. A nil Value checks
// key existence only.
type KeyFilter struct {
	Key   string
	Value *string
}

// Query carries the GET /api/events parameters.
type Query struct {
	Severity *int
	ID       *int64
	Start    *time.Time
	End      *time.Time
	Filters  []KeyFilter
	Limit    int
}

// KeyValue is one metadata entry on an event.
type KeyValue struct {
	Key   string  `json:"key"`
	Value *string `json:"value"`
}

// Entry is one event record with its metadata keys.
type Entry struct {
	ID        int64      `json:"id"`
	Timestamp time.Time  `json:"timestamp"`
	Severity  int        `json:"severity"`
	Message   string     `json:"message"`
	Keys      []KeyValue `json:"keys"`
}

// Service persists and queries events on the events handle.
type Service struct {
	database      *db.Database
	retentionDays int
	logger        *slog.Logger
}

// New builds a Service on the events handle. RetentionDays must be at
// least 1; construction fails otherwise so miswiring surfaces at startup.
func New(database *db.Database, retentionDays int) (*Service, error) {
	if database == nil || database.Events == nil {
		return nil, fmt.Errorf("events database is not configured")
	}
	if retentionDays < 1 {
		return nil, fmt.Errorf("events retention must be at least 1 day, got %d", retentionDays)
	}
	return &Service{database: database, retentionDays: retentionDays}, nil
}

// SetLogger wires structured logging.
func (s *Service) SetLogger(l *slog.Logger) { s.logger = l }

// Append persists one event with its metadata keys in one transaction.
func (s *Service) Append(ctx context.Context, timestamp time.Time, severity int, message string, keys map[string]string) error {
	if severity < SeverityDebug || severity > SeverityFatal {
		err := fmt.Errorf("invalid event severity %d", severity)
		if s.logger != nil {
			s.logger.Error("Event append failed", "error", err, "severity", severity)
		}
		return err
	}
	err := s.database.Events.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		row := record.EventRecord{Timestamp: timestamp.UTC(), Severity: severity, Message: message}
		if err := transaction.Create(&row).Error; err != nil {
			return fmt.Errorf("create event record: %w", err)
		}
		if len(keys) == 0 {
			return nil
		}
		names := make([]string, 0, len(keys))
		for name := range keys {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			value := keys[name]
			keyRow := record.EventKey{RecordID: row.ID, Key: name, Value: &value}
			if err := transaction.Create(&keyRow).Error; err != nil {
				return fmt.Errorf("create event key: %w", err)
			}
		}
		return nil
	})
	if err != nil && s.logger != nil {
		s.logger.Error("Event append failed", "error", err)
	}
	return err
}

// Prune deletes rows older than the retention window and reclaims pages.
func (s *Service) Prune(ctx context.Context) error {
	return db.PruneEvents(ctx, s.database.Events, s.retentionDays)
}

// Start launches hourly retention pruning. It stops when ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(pruneInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.Prune(context.Background())
			}
		}
	}()
}

func orderByIDDesc() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}, Desc: true}}}
}

// Query runs a filtered event query. ID mode returns at most one entry.
// Otherwise severity/start/end narrow the record scan, key filters
// intersect in Go (two phases, no subselects), and keys assemble per
// record. Limit defaults to DefaultLimit and clamps to MaxLimit.
func (s *Service) Query(ctx context.Context, query Query) ([]Entry, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	if query.ID != nil {
		row, err := gorm.G[record.EventRecord](s.database.Events, clause.Eq{Column: "id", Value: *query.ID}).First(ctx)
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, nil
			}
			return nil, fmt.Errorf("get event: %w", err)
		}
		return s.assemble(ctx, []record.EventRecord{row})
	}
	conditions := []clause.Expression{}
	if query.Severity != nil {
		conditions = append(conditions, clause.Gte{Column: "severity", Value: *query.Severity})
	}
	if query.Start != nil {
		conditions = append(conditions, clause.Gte{Column: "timestamp", Value: query.Start.UTC()})
	}
	if query.End != nil {
		conditions = append(conditions, clause.Lte{Column: "timestamp", Value: query.End.UTC()})
	}
	if len(query.Filters) > 0 {
		matched, err := s.matchingIDs(ctx, query.Filters)
		if err != nil {
			return nil, err
		}
		if len(matched) == 0 {
			return nil, nil
		}
		conditions = append(conditions, clause.IN{Column: "id", Values: matched})
	}
	conditions = append(conditions, orderByIDDesc())
	conditions = append(conditions, clause.Limit{Limit: &limit})
	rows, err := gorm.G[record.EventRecord](s.database.Events, conditions...).Find(ctx)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	return s.assemble(ctx, rows)
}

// matchingIDs intersects record IDs across all key filters in Go.
func (s *Service) matchingIDs(ctx context.Context, filters []KeyFilter) ([]any, error) {
	var intersect map[int64]bool
	for _, filter := range filters {
		conditions := []clause.Expression{clause.Eq{Column: "key", Value: filter.Key}}
		if filter.Value != nil {
			conditions = append(conditions, clause.Eq{Column: "value", Value: *filter.Value})
		}
		rows, err := gorm.G[record.EventKey](s.database.Events, conditions...).Find(ctx)
		if err != nil {
			return nil, fmt.Errorf("match event keys: %w", err)
		}
		current := map[int64]bool{}
		for _, row := range rows {
			current[row.RecordID] = true
		}
		if intersect == nil {
			intersect = current
		} else {
			for id := range intersect {
				if !current[id] {
					delete(intersect, id)
				}
			}
		}
		if len(intersect) == 0 {
			return nil, nil
		}
	}
	matched := make([]any, 0, len(intersect))
	for id := range intersect {
		matched = append(matched, id)
	}
	return matched, nil
}

// assemble loads keys for rows and renders entries newest-first.
func (s *Service) assemble(ctx context.Context, rows []record.EventRecord) ([]Entry, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]any, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	keyRows, err := gorm.G[record.EventKey](s.database.Events, clause.IN{Column: "record_id", Values: ids}).Find(ctx)
	if err != nil {
		return nil, fmt.Errorf("list event keys: %w", err)
	}
	byRecord := map[int64][]KeyValue{}
	for _, keyRow := range keyRows {
		byRecord[keyRow.RecordID] = append(byRecord[keyRow.RecordID], KeyValue{Key: keyRow.Key, Value: keyRow.Value})
	}
	for id := range byRecord {
		sort.Slice(byRecord[id], func(a, b int) bool { return byRecord[id][a].Key < byRecord[id][b].Key })
	}
	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		keys := byRecord[row.ID]
		if keys == nil {
			keys = []KeyValue{}
		}
		entries = append(entries, Entry{
			ID:        row.ID,
			Timestamp: row.Timestamp,
			Severity:  row.Severity,
			Message:   row.Message,
			Keys:      keys,
		})
	}
	return entries, nil
}
