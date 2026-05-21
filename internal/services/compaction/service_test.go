package compaction

import (
	"io"
	"log/slog"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/tokencount"
)

func TestCompactIfNeededSkipsUnderBudgetConversations(t *testing.T) {
	svc := New(tokencount.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	messages := []models.ChatMessage{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}

	compacted, stats, err := svc.CompactIfNeeded(messages, models.ModelId("openai/gpt-4o"), 200000)
	if err != nil {
		t.Fatalf("CompactIfNeeded returned error: %v", err)
	}
	if stats.WasCompacted {
		t.Fatalf("expected under-budget conversation to skip compaction, got %+v", stats)
	}
	if stats.OriginalTokens != stats.CompactedTokens {
		t.Fatalf("expected token counts to remain unchanged, got %+v", stats)
	}
	if len(compacted) != len(messages) {
		t.Fatalf("expected messages to remain unchanged, got %d want %d", len(compacted), len(messages))
	}
	for i := range messages {
		if compacted[i].Content != messages[i].Content {
			t.Fatalf("message %d changed unexpectedly: got %q want %q", i, compacted[i].Content, messages[i].Content)
		}
	}
}
