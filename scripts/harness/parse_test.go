package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readRecorded returns a recorded CLI output from testdata.
func readRecorded(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("recorded output %s: %v", name, err)
	}
	return raw
}

func TestClaudeParseAcceptsRecordedTurn(t *testing.T) {
	raw := readRecorded(t, "claude-phase1.json")
	outcome, err := claudeDriver{}.parseTurn(raw, "ecfe3405-5e0d-4fe0-8a63-d4e6a9f926ee")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if outcome.sessionID != "ecfe3405-5e0d-4fe0-8a63-d4e6a9f926ee" || outcome.finalText != "PROBE COMPLETE" {
		t.Fatalf("outcome: %+v", outcome)
	}
	if _, err := (claudeDriver{}).parseTurn(raw, "00000000-0000-4000-8000-000000000000"); err == nil {
		t.Fatalf("a different expected session must be refused")
	}
}

func TestClaudeParseRejectsDeniedTools(t *testing.T) {
	var result map[string]any
	if err := json.Unmarshal(readRecorded(t, "claude-phase1.json"), &result); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	result["permission_denials"] = []any{map[string]any{"tool_name": "Bash"}}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := (claudeDriver{}).parseTurn(raw, ""); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("denied tool call must fail the turn, got %v", err)
	}
}

func TestClaudeParseRejectsErrorSubtype(t *testing.T) {
	raw := []byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"","session_id":"s"}`)
	if _, err := (claudeDriver{}).parseTurn(raw, ""); err == nil {
		t.Fatalf("an error subtype must fail the turn")
	}
}

func TestCodexParseReadsThreadAndFinalMessage(t *testing.T) {
	outcome, err := codexDriver{}.parseTurn(readRecorded(t, "codex-phase1.jsonl"), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if outcome.sessionID == "" || outcome.finalText != "PROBE COMPLETE" {
		t.Fatalf("outcome: %+v", outcome)
	}
}

func TestCodexParseIgnoresWarningItems(t *testing.T) {
	// The recorded run contains error items that are harness warnings. They must
	// not fail the turn, and the agent message still carries the answer.
	if !strings.Contains(string(readRecorded(t, "codex-phase1.jsonl")), `"type":"error"`) {
		t.Fatalf("recorded fixture no longer carries the warning items this test covers")
	}
	if _, err := (codexDriver{}).parseTurn(readRecorded(t, "codex-phase1.jsonl"), ""); err != nil {
		t.Fatalf("warning items must not fail the turn: %v", err)
	}
}

func TestCodexParseRejectsIncompleteTurn(t *testing.T) {
	var kept []string
	for _, line := range strings.Split(string(readRecorded(t, "codex-phase1.jsonl")), "\n") {
		if !strings.Contains(line, `"turn.completed"`) {
			kept = append(kept, line)
		}
	}
	if _, err := (codexDriver{}).parseTurn([]byte(strings.Join(kept, "\n")), ""); err == nil {
		t.Fatalf("a turn without turn.completed must fail")
	}
}

func TestCodexParseRejectsFailedTurn(t *testing.T) {
	raw := []byte(`{"type":"thread.started","thread_id":"t1"}` + "\n" + `{"type":"turn.failed","error":{"message":"boom"}}`)
	if _, err := (codexDriver{}).parseTurn(raw, ""); err == nil || !strings.Contains(err.Error(), "turn.failed") {
		t.Fatalf("turn.failed must fail the turn, got %v", err)
	}
}

func TestOpenCodeParseReadsSessionAndText(t *testing.T) {
	outcome, err := opencodeDriver{}.parseTurn(readRecorded(t, "opencode-phase1.jsonl"), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.HasPrefix(outcome.sessionID, "ses_") || outcome.finalText != "PROBE COMPLETE" {
		t.Fatalf("outcome: %+v", outcome)
	}
}

func TestOpenCodeParseRejectsFailedTool(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"step_start","sessionID":"ses_1","part":{"type":"step-start"}}`,
		`{"type":"tool_use","sessionID":"ses_1","part":{"type":"tool","tool":"bash","state":{"status":"error"}}}`,
		`{"type":"step_finish","sessionID":"ses_1","part":{"type":"step-finish","reason":"stop"}}`,
	}, "\n")
	if _, err := (opencodeDriver{}).parseTurn([]byte(raw), ""); err == nil || !strings.Contains(err.Error(), "bash") {
		t.Fatalf("a failed tool must fail the turn, got %v", err)
	}
}

func TestOpenCodeParseRejectsUnfinishedStep(t *testing.T) {
	raw := `{"type":"text","sessionID":"ses_1","part":{"type":"text","text":"partial"}}`
	if _, err := (opencodeDriver{}).parseTurn([]byte(raw), ""); err == nil {
		t.Fatalf("a step without a stop reason must fail the turn")
	}
}
