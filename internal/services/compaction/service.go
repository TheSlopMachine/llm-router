package compaction

import (
	"log/slog"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/tokencount"
)

type Service struct {
	tokenCounter *tokencount.Service
	logger       *slog.Logger
}

type Stats struct {
	OriginalTokens  int
	CompactedTokens int
	Budget          int
	TokensSaved     int
	MessagesKept    int
	MessagesTrimmed int
	MessagesDropped int
	WasCompacted    bool
}

func New(tokenCounter *tokencount.Service, logger *slog.Logger) *Service {
	return &Service{
		tokenCounter: tokenCounter,
		logger:       logger,
	}
}

func (s *Service) CompactIfNeeded(messages []models.ChatMessage, modelID models.ModelId, contextWindow int64) ([]models.ChatMessage, *Stats, error) {
	originalTokens := s.tokenCounter.CountMessages(messages, modelID)
	budget := int(float64(contextWindow) * 0.85)

	compactionMsgs := s.convertToCompactionMessages(messages, modelID)
	result := Compact(compactionMsgs, budget, true)

	if result.Unreachable {
		s.logger.Warn("conversation exceeds context window even after maximum compaction",
			"floor_tokens", result.Floor,
			"budget", budget,
			"model", modelID)
	}

	compactedMessages := s.applyCompactionResult(messages, result)
	stats := s.calculateStats(result, originalTokens, budget)
	stats.WasCompacted = result.Used < originalTokens

	return compactedMessages, stats, nil
}

func (s *Service) convertToCompactionMessages(messages []models.ChatMessage, modelID models.ModelId) []Message {
	n := len(messages)
	compactionMsgs := make([]Message, n)

	lastUserIdx := -1
	lastAssistantIdx := -1

	for i := n - 1; i >= 0; i-- {
		if messages[i].Role == "user" && lastUserIdx == -1 {
			lastUserIdx = i
		}
		if messages[i].Role == "assistant" && lastAssistantIdx == -1 {
			lastAssistantIdx = i
		}
	}

	for i := 0; i < n; i++ {
		msg := messages[i]
		age := n - 1 - i
		tokens := s.tokenCounter.Count(msg.Content, modelID)

		cat := s.categorizeMessage(msg, i, age, lastUserIdx, lastAssistantIdx, n)
		shout := s.detectShout(msg.Content)
		trunc := s.detectTruncatable(msg.Content)

		compactionMsgs[i] = Message{
			ID:    i,
			Role:  msg.Role,
			Age:   age,
			Tok:   tokens,
			Cat:   cat,
			Shout: shout,
			Trunc: trunc,
			Text:  msg.Content,
		}
	}

	return compactionMsgs
}

func (s *Service) categorizeMessage(msg models.ChatMessage, idx, age, lastUserIdx, lastAssistantIdx, total int) Category {
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		return CatGarbage
	}

	if msg.Role == "system" {
		return CatSystem
	}

	if idx == lastUserIdx {
		return CatHighest
	}

	if idx == lastAssistantIdx {
		return CatHigh
	}

	if age <= 6 {
		return CatMid
	}

	if age <= 10 {
		return CatLow
	}

	return CatLowest
}

func (s *Service) detectShout(content string) bool {
	if len(content) < 10 {
		return false
	}

	if strings.Contains(content, "!!!") {
		return true
	}

	if strings.Contains(content, "IMPORTANT:") || strings.Contains(content, "CRITICAL:") || strings.Contains(content, "URGENT:") {
		return true
	}

	upperCount := 0
	letterCount := 0
	for _, r := range content {
		if r >= 'A' && r <= 'Z' {
			upperCount++
			letterCount++
		} else if r >= 'a' && r <= 'z' {
			letterCount++
		}
	}

	if letterCount > 20 && float64(upperCount)/float64(letterCount) > 0.5 {
		return true
	}

	return false
}

func (s *Service) detectTruncatable(content string) bool {
	if strings.Contains(content, "```") {
		return true
	}

	if len(content) > 1000 {
		return true
	}

	return false
}

func (s *Service) applyCompactionResult(messages []models.ChatMessage, result Result) []models.ChatMessage {
	compacted := make([]models.ChatMessage, 0, len(messages))

	for i, msg := range messages {
		state := result.States[i]

		switch state {
		case StateKept:
			compacted = append(compacted, msg)
		case StateTrim25:
			compacted = append(compacted, models.ChatMessage{
				Role:    msg.Role,
				Content: s.truncateContent(msg.Content, 0.25),
			})
		case StateTrim10:
			compacted = append(compacted, models.ChatMessage{
				Role:    msg.Role,
				Content: s.truncateContent(msg.Content, 0.10),
			})
		case StateDrop:
			continue
		}
	}

	return compacted
}

func (s *Service) truncateContent(content string, percentage float64) string {
	targetLen := int(float64(len(content)) * percentage)
	if targetLen >= len(content) {
		return content
	}
	return content[:targetLen] + "\n[... content truncated to fit context window ...]"
}

func (s *Service) calculateStats(result Result, originalTokens int, budget int) *Stats {
	kept := 0
	trimmed := 0
	dropped := 0

	for _, state := range result.States {
		switch state {
		case StateKept:
			kept++
		case StateTrim25, StateTrim10:
			trimmed++
		case StateDrop:
			dropped++
		}
	}

	return &Stats{
		OriginalTokens:  originalTokens,
		CompactedTokens: result.Used,
		Budget:          budget,
		TokensSaved:     originalTokens - result.Used,
		MessagesKept:    kept,
		MessagesTrimmed: trimmed,
		MessagesDropped: dropped,
	}
}

