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
	"regexp"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
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

// Create creates a new agent with a slug ID derived from its name.
func (s *Service) Create(agent *models.Agent) error {
	// Validate first so slug errors reference a valid name.
	if strings.TrimSpace(agent.Name) == "" {
		return fmt.Errorf("agent name is required")
	}
	id, err := s.uniqueSlug(agent.Name)
	if err != nil {
		return err
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

// uniqueSlug derives a free agent ID from a name, suffixing on collision
// or reserved words. IDs stay stable across renames.
func (s *Service) uniqueSlug(name string) (string, error) {
	base := agentSlug(name)
	if base == "" {
		return "", fmt.Errorf("agent name %q has no usable characters for an ID", name)
	}
	id := base
	for counter := 2; ; counter++ {
		if reservedAgentSlugs[id] {
			id = fmt.Sprintf("%s-%d", base, counter)
			continue
		}
		exists, err := s.repo.Exists(id)
		if err != nil {
			return "", fmt.Errorf("check agent ID: %w", err)
		}
		if !exists {
			return id, nil
		}
		id = fmt.Sprintf("%s-%d", base, counter)
	}
}

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

var uuidHexReg = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// isUUIDLike reports whether id looks like a pre-slug UUID agent ID.
func isUUIDLike(id string) bool {
	clean := strings.ReplaceAll(id, "-", "")
	return len(clean) == 32 && uuidHexReg.MatchString(clean)
}

// MigrateIDs renames UUID-era agents to slug IDs and rewrites every
// reference: credentials data.agent_id and token AllowedModels entries.
// Idempotent: re-runs find no UUID rows and change nothing.
func (s *Service) MigrateIDs(credSvc *credential.Service, tokenSvc *token.Service) (int, error) {
	agents, err := s.List()
	if err != nil {
		return 0, err
	}
	renames := map[string]string{}
	for _, a := range agents {
		if !isUUIDLike(a.ID) {
			continue
		}
		newID, err := s.uniqueSlug(a.Name)
		if err != nil {
			return 0, fmt.Errorf("slug for agent %q: %w", a.Name, err)
		}
		updated := *a
		updated.ID = newID
		updated.UpdatedAt = util.Now()
		if err := s.repo.Put(newID, &updated); err != nil {
			return 0, fmt.Errorf("store renamed agent: %w", err)
		}
		if err := s.repo.Delete(a.ID); err != nil {
			return 0, fmt.Errorf("remove old agent ID: %w", err)
		}
		renames[a.ID] = newID
	}
	if len(renames) == 0 {
		return 0, nil
	}

	creds, err := credSvc.ListAll()
	if err != nil {
		return 0, fmt.Errorf("list credentials: %w", err)
	}
	for _, c := range creds {
		agentID, _ := c.Data["agent_id"].(string)
		newID, ok := renames[agentID]
		if !ok {
			continue
		}
		data := make(map[string]any, len(c.Data))
		for k, v := range c.Data {
			data[k] = v
		}
		data["agent_id"] = newID
		if err := credSvc.Update(c.ID, data, c.ExpiresAt); err != nil {
			return 0, fmt.Errorf("rewrite credential %s: %w", c.ID, err)
		}
	}

	tokens, err := tokenSvc.List()
	if err != nil {
		return 0, fmt.Errorf("list tokens: %w", err)
	}
	for _, tok := range tokens {
		changed := false
		rules := tok.Rules
		for i, m := range rules.AllowedModels {
			providerID, name, err := m.Parse()
			if err != nil || providerID != "agents" {
				continue
			}
			if newID, ok := renames[name]; ok {
				rules.AllowedModels[i] = models.ModelId("agents/" + newID)
				changed = true
			}
		}
		if changed {
			if err := tokenSvc.UpdateRules(tok.ID, rules); err != nil {
				return 0, fmt.Errorf("rewrite token %s: %w", tok.ID, err)
			}
		}
	}

	return len(renames), nil
}
