package luaplugin

import (
	"fmt"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// TestHandlerCall_CorruptSourceReportsParserError simulates a post-install
// corrupted DB row: the stored source no longer parses. The failure must
// carry the parser diagnostics and the source length, never bare "nil".
func TestHandlerCall_CorruptSourceReportsParserError(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(testPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	records, err := svc.List()
	if err != nil || len(records) == 0 {
		t.Fatalf("list: %v", records)
	}
	rec := records[0]
	broken := "--- @plugin Broken\nlocal x = {{{ unterminated"
	rec.Source = []byte(broken)
	if err := svc.repo.Put(rec.ID, rec); err != nil {
		t.Fatal(err)
	}
	if err := svc.rebuild(); err != nil {
		t.Fatal(err)
	}

	cred := &models.Credential{ID: "c1", Data: map[string]any{}}
	_, callErr := svc.Complete(t.Context(), rec.TypeKeys[0], cred, &models.ChatCompletionRequest{
		Model:    "test/broken-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if callErr == nil {
		t.Fatal("corrupt source must fail the call")
	}
	msg := callErr.Error()
	if strings.Contains(msg, "load: nil") || strings.HasSuffix(strings.TrimSpace(msg), "nil") {
		t.Fatalf("load failure must not report bare nil: %q", msg)
	}
	if !strings.Contains(msg, "syntax") {
		t.Fatalf("load failure must carry parser diagnostics: %q", msg)
	}
	if !strings.Contains(msg, fmt.Sprintf("source %d bytes", len(broken))) {
		t.Fatalf("load failure must report source length: %q", msg)
	}
	crashes := svc.Crashes(rec.ID)
	if len(crashes) == 0 {
		t.Fatal("load failure must be recorded as a crash")
	}
	if !strings.Contains(crashes[0].Cause, "syntax") {
		t.Fatalf("crash cause must carry parser diagnostics: %q", crashes[0].Cause)
	}
}
