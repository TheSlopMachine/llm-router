package virtual

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	stdErrors "errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// GormService manages virtual models on the SQLite handle with explicit
// converters between wire types and the virtual_models base rows plus
// position-ordered virtual_model_members rows. Provider existence checks
// run as typed reads inside each write transaction. Managed sync reads the
// provider registry and the model info merged view. Traditional GORM use:
// none; every statement runs through gorm.G with typed clauses.
type GormService struct {
	database         *db.Database
	providerService  *provider.GormService
	modelInfoService *modelinfo.GormService
	logger           *slog.Logger
}

// NewGorm constructs a GORM-backed virtual model service on the SQLite
// handle. Construction performs no I/O.
func NewGorm(database *db.Database, providerService *provider.GormService, modelInfoService *modelinfo.GormService) *GormService {
	return &GormService{database: database, providerService: providerService, modelInfoService: modelInfoService}
}

// SetLogger wires structured logging (called once from server.New).
func (s *GormService) SetLogger(l *slog.Logger) { s.logger = l }

func (s *GormService) log() *slog.Logger {
	if s.logger == nil {
		return slog.Default()
	}
	return s.logger
}

// LiveMembers returns the model ids to try for the agent, snapshotted for
// one request. Managed virtual models resolve their endpoint group live
// from the provider model list (disabled and removed models drop out on
// the next call); manual models use the stored list. Storage failures and
// corrupt markers are errors; a managed group with no models is an empty
// list without an error.
func (s *GormService) LiveMembers(ctx context.Context, agent *models.VirtualModel) ([]models.ModelId, error) {
	if agent == nil {
		return nil, fmt.Errorf("nil virtual model")
	}
	if !strings.HasPrefix(agent.ManagedBy, "provider:") {
		memberIDs := make([]models.ModelId, 0, len(agent.Models))
		for _, e := range agent.Models {
			memberIDs = append(memberIDs, e.ModelID)
		}
		return memberIDs, nil
	}
	providerID, slug, err := ParseMarker(agent.ManagedBy)
	if err != nil {
		return nil, err
	}
	groups, err := s.GroupsForProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		if group.Endpoint != slug {
			continue
		}
		memberIDs := make([]models.ModelId, 0, len(group.Models))
		for _, name := range group.Models {
			memberIDs = append(memberIDs, models.ModelId(providerID+"/"+name))
		}
		return memberIDs, nil
	}
	return nil, nil
}

// orderVirtualModelsByIdentifier sorts virtual model rows by primary key.
func orderVirtualModelsByIdentifier() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}}}}
}

// orderVirtualMembersByPosition sorts member rows by fall-through position.
func orderVirtualMembersByPosition() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "position"}}}}
}

// orderVirtualMembersByModelAndPosition sorts member rows by owning model
// then fall-through position for single-pass grouping in List.
func orderVirtualMembersByModelAndPosition() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "virtual_model_id"}}, {Column: clause.Column{Name: "position"}}}}
}

// requireHandle fails closed when the SQLite handle is missing.
func (s *GormService) requireHandle() (*db.Database, error) {
	if s == nil || s.database == nil || s.database.Handle == nil {
		return nil, fmt.Errorf("virtual sqlite handle is not configured: %w", apierrors.ErrInvalidInput)
	}
	return s.database, nil
}

// validateVirtualModel checks the storage-independent rules with the previous
// messages: name required, at least one member, parseable member IDs, no
// virtual-to-virtual references, no duplicate members. Provider existence
// stays out: the write transaction checks it against provider_instances.
// An empty instruction normalizes to empty.
func validateVirtualModel(virtualModel *models.VirtualModel) error {
	if strings.TrimSpace(virtualModel.Name) == "" {
		return fmt.Errorf("virtual model name is required")
	}
	if len(virtualModel.Models) == 0 {
		return fmt.Errorf("virtual model must have at least one model")
	}
	seen := make(map[string]bool)
	for index, entry := range virtualModel.Models {
		providerID, _, err := entry.ModelID.Parse()
		if err != nil {
			return fmt.Errorf("model %d: invalid model ID: %w", index, err)
		}
		if providerID == provider.TypeVirtual || providerID == "agents" {
			return fmt.Errorf("model %d: virtual models cannot reference other virtual models (circular dependency)", index)
		}
		if seen[string(entry.ModelID)] {
			return fmt.Errorf("model %d: duplicate model ID %q", index, entry.ModelID)
		}
		seen[string(entry.ModelID)] = true
	}
	if strings.TrimSpace(virtualModel.Instruction) == "" {
		virtualModel.Instruction = ""
	}
	return nil
}

// checkVirtualMemberProviders verifies every distinct member provider reads
// present in provider_instances on the transaction handle. A miss reports
// the previous provider message with the member index; storage failures
// surface translated. The foreign keys recheck this on insert.
func checkVirtualMemberProviders(ctx context.Context, transaction *gorm.DB, entries []models.VirtualModelEntry) error {
	checked := make(map[string]bool)
	for index, entry := range entries {
		providerID, _, err := entry.ModelID.Parse()
		if err != nil {
			return fmt.Errorf("model %d: invalid model ID: %w", index, err)
		}
		if checked[providerID] {
			continue
		}
		checked[providerID] = true
		if _, err := gorm.G[record.ProviderInstance](transaction, clause.Eq{Column: "id", Value: providerID}).First(ctx); err != nil {
			if stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
				return fmt.Errorf("model %d: provider %q not found", index, providerID)
			}
			return db.Translate(err)
		}
	}
	return nil
}

// uniqueVirtualSlugInTransaction derives the next free virtual model ID
// from a name on the transaction handle. The scheme mirrors the previous
// uniqueSlug: the slugified name, with a numeric suffix on reserved words
// and collisions. IDs stay stable across renames.
func uniqueVirtualSlugInTransaction(ctx context.Context, transaction *gorm.DB, name string) (string, error) {
	base := agentSlug(name)
	if base == "" {
		return "", fmt.Errorf("virtual model name %q has no usable characters for an ID", name)
	}
	identifier := base
	for counter := 2; ; counter++ {
		if reservedAgentSlugs[identifier] {
			identifier = fmt.Sprintf("%s-%d", base, counter)
			continue
		}
		_, err := gorm.G[record.VirtualModel](transaction, clause.Eq{Column: "id", Value: identifier}).First(ctx)
		if err != nil {
			if stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
				return identifier, nil
			}
			return "", db.Translate(err)
		}
		identifier = fmt.Sprintf("%s-%d", base, counter)
	}
}

// Create persists a new virtual model with a slug ID derived from its name.
// Members insert position-ordered behind the base row; the name_key UNIQUE
// index rejects case-insensitive name duplicates with the previous message
// plus the already-exists sentinel. A dangling member provider fails
// through the existence check as invalid input. One transaction holds the
// slug loop, the provider check, and both inserts on the transaction handle
// only.
func (s *GormService) Create(ctx context.Context, virtualModel *models.VirtualModel) error {
	if virtualModel == nil {
		return fmt.Errorf("virtual model is required: %w", apierrors.ErrInvalidInput)
	}
	if err := validateVirtualModel(virtualModel); err != nil {
		return err
	}
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	return s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		identifier, err := uniqueVirtualSlugInTransaction(ctx, transaction, virtualModel.Name)
		if err != nil {
			return err
		}
		if err := checkVirtualMemberProviders(ctx, transaction, virtualModel.Models); err != nil {
			return err
		}
		now := util.Now()
		virtualModel.ID = identifier
		virtualModel.Version = 1
		virtualModel.CreatedAt = now
		virtualModel.UpdatedAt = now
		row, members, err := virtualRecordFromModel(virtualModel, identifier)
		if err != nil {
			return err
		}
		if err := gorm.G[record.VirtualModel](transaction).Create(ctx, &row); err != nil {
			translated := db.Translate(err)
			if stdErrors.Is(translated, apierrors.ErrAlreadyExists) {
				return fmt.Errorf("virtual model name %q already exists: %w", virtualModel.Name, apierrors.ErrAlreadyExists)
			}
			return translated
		}
		if len(members) > 0 {
			if err := gorm.G[record.VirtualModelMember](transaction).CreateInBatches(ctx, &members, 500); err != nil {
				return db.Translate(err)
			}
		}
		return nil
	})
}

// Get returns a virtual model by ID with members in fall-through order. A
// miss reports the not-found sentinel with virtual model context. One
// transaction holds the base read and the member read on the transaction
// handle only.
func (s *GormService) Get(ctx context.Context, identifier string) (*models.VirtualModel, error) {
	if _, err := s.requireHandle(); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(identifier)
	var result *models.VirtualModel
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		row, err := gorm.G[record.VirtualModel](transaction, clause.Eq{Column: "id", Value: trimmed}).First(ctx)
		if err != nil {
			translated := db.Translate(err)
			if stdErrors.Is(translated, apierrors.ErrNotFound) {
				return apierrors.NewNotFoundError("virtual model", identifier)
			}
			return translated
		}
		members, err := gorm.G[record.VirtualModelMember](transaction, clause.Eq{Column: "virtual_model_id", Value: trimmed}, orderVirtualMembersByPosition()).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		result = virtualModelFromRecords(row, members)
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// List returns all virtual models sorted by ID with members in
// fall-through order. One transaction holds the base read and the member
// read on the transaction handle only; members group in Go. Empty tables
// yield an empty slice, never an error.
func (s *GormService) List(ctx context.Context) ([]*models.VirtualModel, error) {
	if _, err := s.requireHandle(); err != nil {
		return nil, err
	}
	var result []*models.VirtualModel
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		rows, err := gorm.G[record.VirtualModel](transaction, orderVirtualModelsByIdentifier()).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		memberRows, err := gorm.G[record.VirtualModelMember](transaction, orderVirtualMembersByModelAndPosition()).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		membersByModel := make(map[string][]record.VirtualModelMember)
		for _, member := range memberRows {
			membersByModel[member.VirtualModelID] = append(membersByModel[member.VirtualModelID], member)
		}
		items := make([]*models.VirtualModel, 0, len(rows))
		for _, row := range rows {
			items = append(items, virtualModelFromRecords(row, membersByModel[row.ID]))
		}
		result = items
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// Update replaces a virtual model under a stable ID. Member rows replace
// wholesale so removed positions disappear. Optimistic locking mirrors the
// previous path: a nonzero caller version must match storage. Renames onto
// another model name fail through the name_key index with the previous message
// plus the already-exists sentinel. One transaction holds the read, the
// provider check, the base upsert, the member delete, and the member insert
// on the transaction handle only.
func (s *GormService) Update(ctx context.Context, identifier string, virtualModel *models.VirtualModel) error {
	if virtualModel == nil {
		return fmt.Errorf("virtual model is required: %w", apierrors.ErrInvalidInput)
	}
	virtualModel.ID = identifier
	if err := validateVirtualModel(virtualModel); err != nil {
		return err
	}
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	trimmed := strings.TrimSpace(identifier)
	return s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		existing, err := gorm.G[record.VirtualModel](transaction, clause.Eq{Column: "id", Value: trimmed}).First(ctx)
		if err != nil {
			translated := db.Translate(err)
			if stdErrors.Is(translated, apierrors.ErrNotFound) {
				return apierrors.NewNotFoundError("virtual model", identifier)
			}
			return translated
		}
		if virtualModel.Version != 0 && virtualModel.Version != existing.Version {
			return fmt.Errorf("virtual model was modified by another process, please refresh and try again")
		}
		if err := checkVirtualMemberProviders(ctx, transaction, virtualModel.Models); err != nil {
			return err
		}
		updated := *virtualModel
		updated.ID = existing.ID
		updated.CreatedAt = existing.CreatedAt.UTC()
		updated.UpdatedAt = util.Now()
		updated.Version = existing.Version + 1
		row, members, err := virtualRecordFromModel(&updated, existing.ID)
		if err != nil {
			return err
		}
		rows := []record.VirtualModel{row}
		if err := gorm.G[record.VirtualModel](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
			translated := db.Translate(err)
			if stdErrors.Is(translated, apierrors.ErrAlreadyExists) {
				return fmt.Errorf("virtual model name %q already exists: %w", virtualModel.Name, apierrors.ErrAlreadyExists)
			}
			return translated
		}
		if _, err := gorm.G[record.VirtualModelMember](transaction, clause.Eq{Column: "virtual_model_id", Value: trimmed}).Delete(ctx); err != nil {
			return db.Translate(err)
		}
		if len(members) > 0 {
			if err := gorm.G[record.VirtualModelMember](transaction).CreateInBatches(ctx, &members, 500); err != nil {
				return db.Translate(err)
			}
		}
		*virtualModel = updated
		return nil
	})
}

// Delete removes a virtual model by ID in one conditioned delete. Member
// rows cascade from the virtual model foreign key; no member scan runs. A
// zero-row delete reports the not-found sentinel through the WithResult
// tracker.
func (s *GormService) Delete(ctx context.Context, identifier string) error {
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	trimmed := strings.TrimSpace(identifier)
	return s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		tracker := gorm.WithResult()
		affected, err := gorm.G[record.VirtualModel](transaction, clause.Eq{Column: "id", Value: trimmed}, tracker).Delete(ctx)
		if err != nil {
			return db.Translate(err)
		}
		if affected == 0 || tracker.RowsAffected == 0 {
			return fmt.Errorf("virtual model %q: %w", trimmed, apierrors.ErrNotFound)
		}
		return nil
	})
}
