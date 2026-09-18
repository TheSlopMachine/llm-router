// Package virtual implements the Virtual Models service.
//
// Responsibilities:
//   - CRUD operations for Agent records
//   - Validation of virtual model configurations
//   - Aggregated limits and capabilities across the fall-through list
package virtual

import (
	"context"
	"fmt"
	"regexp"
	"sort"
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
	db           *db.DB
	repo         *repository.Repository[models.VirtualModel]
	providerSvc  *provider.Service
	modelInfoSvc *modelinfo.Service
}

// New constructs a new virtual-model Service.
func New(database *db.DB, providerSvc *provider.Service, modelInfoSvc *modelinfo.Service) *Service {
	return &Service{
		db:           database,
		repo:         repository.New[models.VirtualModel](database, db.BucketVirtualModels, "virtual model"),
		providerSvc:  providerSvc,
		modelInfoSvc: modelInfoSvc,
	}
}

// ─────────────────────────────────────────────
// CRUD Operations
// ─────────────────────────────────────────────

// Create creates a new virtual model with a slug ID derived from its name.
func (s *Service) Create(vm *models.VirtualModel) error {
	// Validate first so slug errors reference a valid name.
	if strings.TrimSpace(vm.Name) == "" {
		return fmt.Errorf("virtual model name is required")
	}
	id, err := s.uniqueSlug(vm.Name)
	if err != nil {
		return err
	}
	vm.ID = id

	// Validate
	if err := s.validate(vm); err != nil {
		return err
	}

	// Check for duplicate name
	if err := s.checkUniqueName(vm); err != nil {
		return err
	}

	// Aggregated limits and capabilities across the fall-through list
	s.computeAggregates(vm)

	// Set initial version and timestamps
	vm.Version = 1
	now := util.Now()
	vm.CreatedAt = now
	vm.UpdatedAt = now

	// Persist
	return s.repo.Put(vm.ID, vm)
}

// Get retrieves a virtual model by ID.
func (s *Service) Get(id string) (*models.VirtualModel, error) {
	return s.repo.Get(id)
}

// List returns all virtual models.
func (s *Service) List() ([]*models.VirtualModel, error) {
	return s.repo.List()
}

// Update updates an existing virtual model.
func (s *Service) Update(id string, vm *models.VirtualModel) error {
	vm.ID = id

	// Validate
	if err := s.validate(vm); err != nil {
		return err
	}

	// Check for duplicate name (excluding self)
	if err := s.checkUniqueName(vm); err != nil {
		return err
	}

	// Aggregated limits and capabilities across the fall-through list
	s.computeAggregates(vm)

	// Update via repository with optimistic locking
	return s.repo.Update(id, func(existing *models.VirtualModel) error {
		// Optimistic locking check
		if vm.Version != 0 && vm.Version != existing.Version {
			return fmt.Errorf("virtual model was modified by another process, please refresh and try again")
		}

		vm.ID = existing.ID
		vm.CreatedAt = existing.CreatedAt
		vm.UpdatedAt = util.Now()
		vm.Version = existing.Version + 1
		*existing = *vm
		return nil
	})
}

// Delete removes a virtual model by ID.
func (s *Service) Delete(id string) error {
	return s.repo.Delete(id)
}

// ─────────────────────────────────────────────
// Validation
// ─────────────────────────────────────────────

func (s *Service) validate(vm *models.VirtualModel) error {
	// Name required
	if strings.TrimSpace(vm.Name) == "" {
		return fmt.Errorf("virtual model name is required")
	}

	// At least one model required — a virtual model without models cannot route
	if len(vm.Models) == 0 {
		return fmt.Errorf("virtual model must have at least one model")
	}

	if err := s.validateModels(vm.Models); err != nil {
		return err
	}

	// Normalize empty instruction
	if strings.TrimSpace(vm.Instruction) == "" {
		vm.Instruction = ""
	}

	return nil
}

func (s *Service) validateModels(models []models.VirtualModelEntry) error {
	seen := make(map[string]bool)

	for i, model := range models {
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
		_, err = s.providerSvc.Get(providerID)
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

var slugStripReg = regexp.MustCompile(`[^a-z0-9-]+`)
var slugDashReg = regexp.MustCompile(`-+`)

// agentSlug converts a name to a URL-safe slug, or "" when unusable.
func agentSlug(name string) string {
	slug := strings.ToLower(name)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	slug = slugStripReg.ReplaceAllString(slug, "")
	slug = strings.Trim(slug, "-")
	slug = slugDashReg.ReplaceAllString(slug, "-")
	return slug
}

// uniqueSlug derives a free vm ID from a name, suffixing on collision
// or reserved words. IDs stay stable across renames.
func (s *Service) uniqueSlug(name string) (string, error) {
	base := agentSlug(name)
	if base == "" {
		return "", fmt.Errorf("vm name %q has no usable characters for an ID", name)
	}
	id := base
	for counter := 2; ; counter++ {
		if reservedAgentSlugs[id] {
			id = fmt.Sprintf("%s-%d", base, counter)
			continue
		}
		exists, err := s.repo.Exists(id)
		if err != nil {
			return "", fmt.Errorf("check vm ID: %w", err)
		}
		if !exists {
			return id, nil
		}
		id = fmt.Sprintf("%s-%d", base, counter)
	}
}

// computeAggregates derives the virtual model's advertised limits from its
// fall-through list: capabilities = intersection (a feature is only safe when
// every model has it), context length and max completion tokens = the minima
// (a larger request or a larger output budget would break weaker models).
// Best-effort: models without cached metadata are skipped.
func (s *Service) computeAggregates(vm *models.VirtualModel) {
	ctx := context.Background()

	capCount := map[string]int{}
	var contextLength, maxCompletionTokens int64

	for _, model := range vm.Models {
		mi, err := s.modelInfoSvc.GetModelInfo(ctx, model.ModelID)
		if err != nil {
			continue
		}
		for _, cap := range mi.Capabilities {
			capCount[cap]++
		}
		if mi.ContextWindow > 0 && (contextLength == 0 || mi.ContextWindow < contextLength) {
			contextLength = mi.ContextWindow
		}
		if mi.MaxTokens > 0 && (maxCompletionTokens == 0 || mi.MaxTokens < maxCompletionTokens) {
			maxCompletionTokens = mi.MaxTokens
		}
	}

	caps := make([]string, 0, len(capCount))
	for cap, n := range capCount {
		if n == len(vm.Models) {
			caps = append(caps, cap)
		}
	}
	sort.Strings(caps)

	vm.Capabilities = caps
	vm.ContextLength = contextLength
	vm.MaxCompletionTokens = maxCompletionTokens
}

func (s *Service) checkUniqueName(vm *models.VirtualModel) error {
	existing, err := s.List()
	if err != nil {
		return fmt.Errorf("check unique name: %w", err)
	}

	for _, a := range existing {
		if a.ID != vm.ID && strings.EqualFold(a.Name, vm.Name) {
			return fmt.Errorf("virtual model name %q already exists", vm.Name)
		}
	}

	return nil
}
