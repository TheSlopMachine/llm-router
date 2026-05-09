package tokencount

import (
	"strings"
	"sync"

	"github.com/TheSlopMachine/slop-tokenizer"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

type Service struct {
	tokenizers map[string]*tokenizer.Tokenizer
	mu         sync.RWMutex
}

func New() *Service {
	return &Service{
		tokenizers: make(map[string]*tokenizer.Tokenizer),
	}
}

func (s *Service) Count(text string, modelID models.ModelId) int {
	tok, err := s.getTokenizer(modelID)
	if err != nil {
		return len(text) / 4
	}
	return tok.Count(text)
}

func (s *Service) CountMessages(messages []models.ChatMessage, modelID models.ModelId) int {
	total := 0
	for _, msg := range messages {
		total += s.Count(msg.Content, modelID)
		total += 4
	}
	return total
}

func (s *Service) getTokenizer(modelID models.ModelId) (*tokenizer.Tokenizer, error) {
	encoding := s.getEncodingForModel(modelID)

	s.mu.RLock()
	tok, exists := s.tokenizers[encoding]
	s.mu.RUnlock()

	if exists {
		return tok, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tok, exists = s.tokenizers[encoding]
	if exists {
		return tok, nil
	}

	newTok, err := tokenizer.New(encoding)
	if err != nil {
		return nil, err
	}

	s.tokenizers[encoding] = newTok
	return newTok, nil
}

func (s *Service) getEncodingForModel(modelID models.ModelId) string {
	providerID, modelName, err := modelID.Parse()
	if err != nil {
		return tokenizer.O200K_BASE
	}

	adapterType := providerID
	if idx := strings.Index(providerID, ":"); idx != -1 {
		adapterType = providerID[:idx]
	}

	modelLower := strings.ToLower(modelName)

	switch adapterType {
	case "openai":
		if strings.Contains(modelLower, "gpt-4o") {
			return tokenizer.O200K_BASE
		}
		if strings.Contains(modelLower, "gpt-4") {
			return tokenizer.CL100K_BASE
		}
		if strings.Contains(modelLower, "gpt-3.5") {
			return tokenizer.CL100K_BASE
		}
		return tokenizer.O200K_BASE
	case "anthropic":
		return tokenizer.CLAUDE
	default:
		return tokenizer.O200K_BASE
	}
}

