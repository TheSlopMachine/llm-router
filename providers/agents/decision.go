package agents

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// routeWithDecisionModel uses a decision model to intelligently select
// the best model for the request.
func (a *Adapter) routeWithDecisionModel(
	ctx context.Context,
	agent *models.Agent,
	req *models.ChatCompletionRequest,
) *models.AgentModel {
	routerSvc := a.getRouterService()
	if routerSvc == nil {
		return nil
	}

	logger := a.getLogger()

	// Build decision prompt
	decisionPrompt := buildDecisionPrompt(agent, req)

	decisionReq := &models.ChatCompletionRequest{
		Model:       agent.DecisionModel.ModelID,
		Messages:    decisionPrompt,
		MaxTokens:   20, // Just need a number
		Temperature: 0.0, // Deterministic
	}

	// Make decision request
	resp, err := routerSvc.Complete(ctx, decisionReq)
	if err != nil {
		logger.Warn("decision model failed, falling back to priority",
			"agent", agent.Name,
			"decision_model", agent.DecisionModel.ModelID,
			"error", err)
		return nil
	}

	// Parse response
	choice := parseDecisionResponse(resp, len(agent.Models))
	if choice < 0 {
		logger.Warn("decision model returned invalid response, falling back to priority",
			"agent", agent.Name,
			"response", resp.Choices[0].Message.Content)
		return nil
	}

	// Sort models by priority to match the numbering in the prompt
	sortedModels := make([]models.AgentModel, len(agent.Models))
	copy(sortedModels, agent.Models)
	// Sort by priority (already done in reorderModels, but we need it here too)
	for i := 0; i < len(sortedModels); i++ {
		for j := i + 1; j < len(sortedModels); j++ {
			if sortedModels[j].Priority < sortedModels[i].Priority {
				sortedModels[i], sortedModels[j] = sortedModels[j], sortedModels[i]
			}
		}
	}

	return &sortedModels[choice]
}

// buildDecisionPrompt constructs the prompt for the decision model.
func buildDecisionPrompt(agent *models.Agent, req *models.ChatCompletionRequest) []models.ChatMessage {
	// Sort models by priority for consistent numbering
	sortedModels := make([]models.AgentModel, len(agent.Models))
	copy(sortedModels, agent.Models)
	for i := 0; i < len(sortedModels); i++ {
		for j := i + 1; j < len(sortedModels); j++ {
			if sortedModels[j].Priority < sortedModels[i].Priority {
				sortedModels[i], sortedModels[j] = sortedModels[j], sortedModels[i]
			}
		}
	}

	// Build system prompt with model descriptions
	systemContent := agent.DecisionModel.SystemPrompt + "\n\nAvailable models:\n"
	for i, model := range sortedModels {
		systemContent += fmt.Sprintf("%d. %s (priority: %d)", i+1, model.ModelID, model.Priority)
		if model.Description != "" {
			systemContent += fmt.Sprintf(" - %s", model.Description)
		}
		systemContent += "\n"
	}
	systemContent += "\nRespond with ONLY the number (1, 2, 3, etc.) of the best model for this request."

	messages := []models.ChatMessage{
		{Role: "system", Content: systemContent},
	}

	// Add user's original messages
	messages = append(messages, req.Messages...)

	return messages
}

// parseDecisionResponse extracts the model choice from the decision model's response.
// Uses lenient parsing with validation.
// Returns -1 if invalid.
func parseDecisionResponse(resp *models.ChatCompletionResponse, modelCount int) int {
	if len(resp.Choices) == 0 {
		return -1
	}

	content := resp.Choices[0].Message.Content

	// Extract first number using regex
	re := regexp.MustCompile(`\d+`)
	match := re.FindString(content)
	if match == "" {
		return -1
	}

	choice, err := strconv.Atoi(match)
	if err != nil {
		return -1
	}

	// Validate: 1-indexed, within range
	if choice < 1 || choice > modelCount {
		return -1
	}

	// Convert to 0-indexed
	return choice - 1
}

