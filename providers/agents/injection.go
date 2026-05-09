package agents

import (
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// injectInstructions injects general agent instructions into the message list.
func injectInstructions(messages []models.ChatMessage, instructions models.AgentInstructions) []models.ChatMessage {
	if instructions.Content == "" {
		return messages
	}

	systemMsg := models.ChatMessage{
		Role:    "system",
		Content: instructions.Content,
	}

	if instructions.Injection == models.InjectionBeginning {
		return append([]models.ChatMessage{systemMsg}, messages...)
	}
	return append(messages, systemMsg)
}

// injectModelInstructions injects model-specific instructions into the message list.
func injectModelInstructions(messages []models.ChatMessage, instructions string) []models.ChatMessage {
	if instructions == "" {
		return messages
	}

	systemMsg := models.ChatMessage{
		Role:    "system",
		Content: instructions,
	}

	// Model-specific instructions always appended at the end
	return append(messages, systemMsg)
}

