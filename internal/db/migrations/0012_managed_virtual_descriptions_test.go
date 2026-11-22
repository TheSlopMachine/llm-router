package migrations

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/migrate"
	"gorm.io/gorm"
)

func TestManagedVirtualDescriptionsRewritesLegacyTextOnly(t *testing.T) {
	ctx := context.Background()
	primary := openIdentityHandle(t, filepath.Join(t.TempDir(), "managed-descriptions.sqlite3"))
	if err := migrate.Run(ctx, primary, RegistryDatabase[:10]); err != nil {
		t.Fatalf("apply earlier migrations: %v", err)
	}
	now := time.Now().UTC()
	provider := providerInstanceSnapshot{ID: "groq", Name: "Groq", TypeKey: "groq", CreatedAt: now, UpdatedAt: now}
	if err := gorm.G[providerInstanceSnapshot](primary).Create(ctx, &provider); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	providerID := "groq"
	chat, speech, transcription := "chat", "speech", "transcription"
	legacy := "Automatic fall-through across Groq chat models. Members refresh on import."
	rows := []virtualModelSnapshot{
		{ID: "groq-chat", Name: "Groq Chat", NameKey: "groq chat", Description: legacy, ManagedProviderID: &providerID, ManagedEndpointSlug: &chat, CreatedAt: now, UpdatedAt: now},
		{ID: "groq-tts", Name: "Groq TTS", NameKey: "groq tts", Description: "Edited by hand.", ManagedProviderID: &providerID, ManagedEndpointSlug: &speech, CreatedAt: now, UpdatedAt: now},
		{ID: "groq-stt", Name: "Groq STT", NameKey: "groq stt", Description: "Automatic fall-through across Groq stt models. Members refresh on import.", ManagedProviderID: &providerID, ManagedEndpointSlug: &transcription, CreatedAt: now, UpdatedAt: now},
		{ID: "mine", Name: "Mine", NameKey: "mine", Description: legacy, CreatedAt: now, UpdatedAt: now},
	}
	for index := range rows {
		if err := gorm.G[virtualModelSnapshot](primary).Create(ctx, &rows[index]); err != nil {
			t.Fatalf("seed virtual model %s: %v", rows[index].ID, err)
		}
	}
	if err := migrate.Run(ctx, primary, RegistryDatabase); err != nil {
		t.Fatalf("apply managed virtual descriptions migration: %v", err)
	}
	stored, err := gorm.G[virtualModelSnapshot](primary).Find(ctx)
	if err != nil {
		t.Fatalf("read virtual models: %v", err)
	}
	descriptions := map[string]string{}
	for _, row := range stored {
		descriptions[row.ID] = row.Description
	}
	if got := descriptions["groq-chat"]; got != "Fall-through across Groq chat models." {
		t.Errorf("managed legacy description = %q, want the short form", got)
	}
	if got := descriptions["groq-stt"]; got != "Fall-through across Groq STT models." {
		t.Errorf("managed legacy acronym description = %q, want STT in upper case", got)
	}
	if got := descriptions["groq-tts"]; got != "Edited by hand." {
		t.Errorf("edited managed description = %q, want kept", got)
	}
	if got := descriptions["mine"]; got != legacy {
		t.Errorf("manual model description = %q, want kept", got)
	}
}
