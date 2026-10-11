// Package virtual implements the Virtual Models service.
//
// Responsibilities:
//   - CRUD operations for Agent records
//   - Validation of virtual model configurations
//   - Aggregated limits and capabilities across the fall-through list
package virtual

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// Service manages virtual model records.
type Service struct {
	db               *db.DB
	store            *repository.Repository[models.VirtualModel]
	providerService  *provider.Service
	modelInfoService *modelinfo.Service
	logger           *slog.Logger
}

// New constructs a new virtual-model Service.
func New(database *db.DB, providerService *provider.Service, modelInfoService *modelinfo.Service) *Service {
	return &Service{
		db:               database,
		store:            repository.New[models.VirtualModel](database, db.BucketVirtualModels, "virtual model"),
		providerService:  providerService,
		modelInfoService: modelInfoService,
	}
}

// SetLogger wires structured logging (called once from server.New).
func (s *Service) SetLogger(l *slog.Logger) { s.logger = l }

func (s *Service) log() *slog.Logger {
	if s.logger == nil {
		return slog.Default()
	}
	return s.logger
}

// ─────────────────────────────────────────────
// CRUD Operations
// ─────────────────────────────────────────────

// Create creates a new virtual model with a slug ID derived from its name.
func (s *Service) Create(virtualModel *models.VirtualModel) error {
	// Validate first so slug errors reference a valid name.
	if strings.TrimSpace(virtualModel.Name) == "" {
		return fmt.Errorf("virtual model name is required")
	}
	id, err := s.uniqueSlug(virtualModel.Name)
	if err != nil {
		return err
	}
	virtualModel.ID = id

	// Validate
	if err := s.validate(virtualModel); err != nil {
		return err
	}

	// Check for duplicate name
	if err := s.checkUniqueName(virtualModel); err != nil {
		return err
	}

	// Set initial version and timestamps
	virtualModel.Version = 1
	now := util.Now()
	virtualModel.CreatedAt = now
	virtualModel.UpdatedAt = now

	// Persist
	return s.store.Put(virtualModel.ID, virtualModel)
}

// Get retrieves a virtual model by ID.
func (s *Service) Get(id string) (*models.VirtualModel, error) {
	return s.store.Get(id)
}

// List returns all virtual models.
func (s *Service) List() ([]*models.VirtualModel, error) {
	return s.store.List()
}

// Update updates an existing virtual model.
func (s *Service) Update(id string, virtualModel *models.VirtualModel) error {
	virtualModel.ID = id

	// Validate
	if err := s.validate(virtualModel); err != nil {
		return err
	}

	// Check for duplicate name (excluding self)
	if err := s.checkUniqueName(virtualModel); err != nil {
		return err
	}

	// Update via repository with optimistic locking
	return s.store.Update(id, func(existing *models.VirtualModel) error {
		// Optimistic locking check
		if virtualModel.Version != 0 && virtualModel.Version != existing.Version {
			return fmt.Errorf("virtual model was modified by another process, please refresh and try again")
		}

		virtualModel.ID = existing.ID
		virtualModel.CreatedAt = existing.CreatedAt
		virtualModel.UpdatedAt = util.Now()
		virtualModel.Version = existing.Version + 1
		*existing = *virtualModel
		return nil
	})
}

// Delete removes a virtual model by ID.
func (s *Service) Delete(id string) error {
	return s.store.Delete(id)
}

// ─────────────────────────────────────────────
// Validation
// ─────────────────────────────────────────────

func (s *Service) validate(virtualModel *models.VirtualModel) error {
	// Name required
	if strings.TrimSpace(virtualModel.Name) == "" {
		return fmt.Errorf("virtual model name is required")
	}

	// At least one model required — a virtual model without models cannot route
	if len(virtualModel.Models) == 0 {
		return fmt.Errorf("virtual model must have at least one model")
	}

	if err := s.validateModels(virtualModel.Models); err != nil {
		return err
	}

	// Normalize empty instruction
	if strings.TrimSpace(virtualModel.Instruction) == "" {
		virtualModel.Instruction = ""
	}

	return nil
}

func (s *Service) validateModels(entries []models.VirtualModelEntry) error {
	seen := make(map[string]bool)

	for i, model := range entries {
		// Check for circular dependency (virtual models referencing virtual models)
		providerID, _, err := model.ModelID.Parse()
		if err != nil {
			return fmt.Errorf("model %d: invalid model ID: %w", i, err)
		}

		if providerID == provider.TypeVirtual || providerID == "agents" {
			return fmt.Errorf("model %d: virtual models cannot reference other virtual models (circular dependency)", i)
		}

		// Check for duplicates
		if seen[string(model.ModelID)] {
			return fmt.Errorf("model %d: duplicate model ID %q", i, model.ModelID)
		}
		seen[string(model.ModelID)] = true

		// Verify provider exists
		_, err = s.providerService.Get(providerID)
		if err != nil {
			return fmt.Errorf("model %d: provider %q not found", i, providerID)
		}
	}

	return nil
}

// ─────────────────────────────────────────────
// Helper Methods
// ─────────────────────────────────────────────

// reservedAgentSlugs collide with dashboard routes.
var reservedAgentSlugs = map[string]bool{"new": true}

// agentSlug converts a name to a URL-safe slug, or "" when unusable.
func agentSlug(name string) string {
	return util.Slugify(name)
}

// uniqueSlug derives a free virtual model ID from a name, suffixing on collision
// or reserved words. IDs stay stable across renames.
func (s *Service) uniqueSlug(name string) (string, error) {
	base := agentSlug(name)
	if base == "" {
		return "", fmt.Errorf("virtual model name %q has no usable characters for an ID", name)
	}
	id := base
	for counter := 2; ; counter++ {
		if reservedAgentSlugs[id] {
			id = fmt.Sprintf("%s-%d", base, counter)
			continue
		}
		exists, err := s.store.Exists(id)
		if err != nil {
			return "", fmt.Errorf("check virtual model ID: %w", err)
		}
		if !exists {
			return id, nil
		}
		id = fmt.Sprintf("%s-%d", base, counter)
	}
}

// LiveMembers returns the model ids to try for the agent, snapshotted for
// one request. Managed virtual models resolve their endpoint group live
// from the provider model list (disabled and removed models drop out on
// the next call); manual models use the stored list. Storage failures and
// corrupt markers are errors; a managed group with no models is an empty
// list without an error.
func (s *Service) LiveMembers(agent *models.VirtualModel) ([]models.ModelId, error) {
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
	groups, err := s.GroupsForProvider(providerID)
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

func (s *Service) checkUniqueName(virtualModel *models.VirtualModel) error {
	existing, err := s.List()
	if err != nil {
		return fmt.Errorf("check unique name: %w", err)
	}

	for _, a := range existing {
		if a.ID != virtualModel.ID && strings.EqualFold(a.Name, virtualModel.Name) {
			return fmt.Errorf("virtual model name %q already exists", virtualModel.Name)
		}
	}

	return nil
}
