// Package agent implements the Agent Service.
//
// Responsibilities:
//   - CRUD operations for Agent records
//   - Validation of agent configurations
//   - Calculation of agent metadata (max tokens)
package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// Service manages agent records.
type Service struct {
	db           *db.DB
	repo         *repository.Repository[models.Agent]
	providerSvc  *provider.Service
	modelInfoSvc *modelinfo.Service
}

// New constructs a new agent Service.
func New(database *db.DB, providerSvc *provider.Service, modelInfoSvc *modelinfo.Service) *Service {
	return &Service{
		db:           database,
		repo:         repository.New[models.Agent](database, db.BucketAgents, "agent"),
		providerSvc:  providerSvc,
		modelInfoSvc: modelInfoSvc,
	}
}

// ─────────────────────────────────────────────
// CRUD Operations
// ─────────────────────────────────────────────

// Create creates a new agent.
func (s *Service) Create(agent *models.Agent) error {
	// Generate ID
	id, err := util.GenerateID()
	if err != nil {
		return fmt.Errorf("generate agent ID: %w", err)
	}
	agent.ID = id

	// Validate
	if err := s.validate(agent); err != nil {
		return err
	}

	// Check for duplicate name
	if err := s.checkUniqueName(agent); err != nil {
		return err
	}

	// Calculate max tokens
	maxTokens, err := s.calculateMaxTokens(agent.Models)
	if err != nil {
		return fmt.Errorf("calculate max tokens: %w", err)
	}
	agent.MaxTokens = maxTokens

	// Set initial version and timestamps
	agent.Version = 1
	now := util.Now()
	agent.CreatedAt = now
	agent.UpdatedAt = now

	// Mark as draft if no models configured
	agent.IsDraft = len(agent.Models) == 0

	// Persist
	return s.repo.Put(agent.ID, agent)
}

// Get retrieves an agent by ID.
func (s *Service) Get(id string) (*models.Agent, error) {
	return s.repo.Get(id)
}

// List returns all agents.
func (s *Service) List() ([]*models.Agent, error) {
	return s.repo.List()
}

// Update updates an existing agent.
func (s *Service) Update(id string, agent *models.Agent) error {
	agent.ID = id

	// Validate
	if err := s.validate(agent); err != nil {
		return err
	}

	// Check for duplicate name (excluding self)
	if err := s.checkUniqueName(agent); err != nil {
		return err
	}

	// Calculate max tokens
	maxTokens, err := s.calculateMaxTokens(agent.Models)
	if err != nil {
		return fmt.Errorf("calculate max tokens: %w", err)
	}
	agent.MaxTokens = maxTokens

	// Update via repository with optimistic locking
	return s.repo.Update(id, func(existing *models.Agent) error {
		// Optimistic locking check
		if agent.Version != 0 && agent.Version != existing.Version {
			return fmt.Errorf("agent was modified by another process, please refresh and try again")
		}

		agent.ID = existing.ID
		agent.CreatedAt = existing.CreatedAt
		agent.UpdatedAt = util.Now()
		agent.Version = existing.Version + 1
		agent.IsDraft = len(agent.Models) == 0
		*existing = *agent
		return nil
	})
}

// Delete removes an agent by ID.
func (s *Service) Delete(id string) error {
	return s.repo.Delete(id)
}

// ─────────────────────────────────────────────
// Validation
// ─────────────────────────────────────────────

func (s *Service) validate(agent *models.Agent) error {
	// Name required
	if strings.TrimSpace(agent.Name) == "" {
		return fmt.Errorf("agent name is required")
	}

	// At least one model required (unless saving as draft)
	if len(agent.Models) == 0 && !agent.IsDraft {
		return fmt.Errorf("agent must have at least one model")
	}

	// Validate models if present
	if len(agent.Models) > 0 {
		if err := s.validateModels(agent.Models); err != nil {
			return err
		}
	}

	// Validate decision model if configured
	if agent.DecisionModel != nil {
		if err := s.validateDecisionModel(agent.DecisionModel); err != nil {
			return err
		}
	}

	// Normalize empty instructions
	if strings.TrimSpace(agent.Instructions.Content) == "" {
		agent.Instructions.Content = ""
	}

	return nil
}

func (s *Service) validateModels(models []models.AgentModel) error {
	seen := make(map[string]bool)

	for i, model := range models {
		// Check for circular dependency (agents referencing agents)
		providerID, _, err := model.ModelID.Parse()
		if err != nil {
			return fmt.Errorf("model %d: invalid model ID: %w", i, err)
		}

		if providerID == "agents" {
			return fmt.Errorf("model %d: agents cannot reference other agents (circular dependency)", i)
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

func (s *Service) validateDecisionModel(config *models.DecisionModelConfig) error {
	// Model ID required
	if config.ModelID == "" {
		return fmt.Errorf("decision model ID is required")
	}

	// System prompt required
	if strings.TrimSpace(config.SystemPrompt) == "" {
		return fmt.Errorf("decision model system prompt is required")
	}

	// Verify provider exists
	providerID, _, err := config.ModelID.Parse()
	if err != nil {
		return fmt.Errorf("decision model: invalid model ID: %w", err)
	}

	// Decision model cannot be an agent
	if providerID == "agents" {
		return fmt.Errorf("decision model cannot be an agent")
	}

	_, err = s.providerSvc.Get(providerID)
	if err != nil {
		return fmt.Errorf("decision model: provider %q not found", providerID)
	}

	return nil
}

// ─────────────────────────────────────────────
// Helper Methods
// ─────────────────────────────────────────────

func (s *Service) calculateMaxTokens(models []models.AgentModel) (int, error) {
	maxTokens := 0
	ctx := context.Background()

	for _, model := range models {
		modelInfo, err := s.modelInfoSvc.GetModelInfo(ctx, model.ModelID)
		if err != nil {
			// Log but don't fail - model info might not be available
			continue
		}

		if modelInfo.MaxTokens > int64(maxTokens) {
			maxTokens = int(modelInfo.MaxTokens)
		}
	}

	return maxTokens, nil
}

func (s *Service) checkUniqueName(agent *models.Agent) error {
	existing, err := s.List()
	if err != nil {
		return fmt.Errorf("check unique name: %w", err)
	}

	for _, a := range existing {
		if a.ID != agent.ID && strings.EqualFold(a.Name, agent.Name) {
			return fmt.Errorf("agent name %q already exists", agent.Name)
		}
	}

	return nil
}

