package main

import (
	"encoding/json"
	"fmt"
)

// claudeDriver runs Claude Code in print mode. The router serves its Messages
// protocol at /v1/messages, so ANTHROPIC_BASE_URL is the admin listener root.
type claudeDriver struct{}

// claudeToolFlags restricts the tool set and allows exactly those tools, so
// headless runs neither prompt nor silently deny the phase tool calls.
var claudeToolFlags = []string{"--tools", "Bash", "Read", "Write", "--allowedTools", "Bash", "Read", "Write"}

func (claudeDriver) name() string      { return "claude" }
func (claudeDriver) binary() string    { return "claude" }
func (claudeDriver) bareModel() string { return "claude-sonnet-4-5" }

// newSessionID pins the session so every invocation can be checked against it.
func (claudeDriver) newSessionID() string { return newUUID() }

func (claudeDriver) prepare(layout runLayout, target routerTarget) ([]string, error) {
	return []string{
		"ANTHROPIC_BASE_URL=" + target.adminURL,
		"ANTHROPIC_API_KEY=ci-unused",
		"CLAUDE_CONFIG_DIR=" + layout.claudeConfig,
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		"DISABLE_AUTOUPDATER=1",
	}, nil
}

func (claudeDriver) initialArguments(turn turnRequest) []string {
	arguments := []string{"-p", turn.prompt, "--output-format", "json", "--session-id", turn.sessionID, "--model", turn.routerModel}
	return append(arguments, claudeToolFlags...)
}

func (claudeDriver) resumeArguments(turn turnRequest) []string {
	arguments := []string{"-p", "--resume", turn.sessionID, turn.prompt, "--output-format", "json", "--model", turn.routerModel}
	return append(arguments, claudeToolFlags...)
}

// claudeResult is the single JSON object `claude -p --output-format json` prints.
type claudeResult struct {
	Subtype           string            `json:"subtype"`
	IsError           bool              `json:"is_error"`
	Result            string            `json:"result"`
	SessionID         string            `json:"session_id"`
	PermissionDenials []json.RawMessage `json:"permission_denials"`
}

func (claudeDriver) parseTurn(stdout []byte, expectedSessionID string) (turnOutcome, error) {
	var result claudeResult
	if err := json.Unmarshal(stdout, &result); err != nil {
		return turnOutcome{}, fmt.Errorf("decode claude output: %w", err)
	}
	if result.IsError || result.Subtype != "success" {
		return turnOutcome{}, fmt.Errorf("claude ended with subtype %q (is_error=%v)", result.Subtype, result.IsError)
	}
	if len(result.PermissionDenials) > 0 {
		return turnOutcome{}, fmt.Errorf("claude denied %d tool call(s)", len(result.PermissionDenials))
	}
	if expectedSessionID != "" && result.SessionID != expectedSessionID {
		return turnOutcome{}, fmt.Errorf("claude session %q, want %q", result.SessionID, expectedSessionID)
	}
	return turnOutcome{sessionID: result.SessionID, finalText: result.Result}, nil
}
