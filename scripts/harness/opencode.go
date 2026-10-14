package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// opencodeDriver runs `opencode run`. The provider sits under
// provider.<id>.options.baseURL with the openai-compatible SDK, which posts to
// the router's /v1/chat/completions. Inline configuration keeps the run free of
// any config file outside the run directory.
type opencodeDriver struct{}

func (opencodeDriver) name() string         { return "opencode" }
func (opencodeDriver) binary() string       { return "opencode" }
func (opencodeDriver) bareModel() string    { return "opencode-harness" }
func (opencodeDriver) newSessionID() string { return "" }

func (opencodeDriver) prepare(layout runLayout, target routerTarget) ([]string, error) {
	configuration := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]any{
			"llm-router": map[string]any{
				"npm":     "@ai-sdk/openai-compatible",
				"name":    "llm-router",
				"options": map[string]any{"baseURL": target.adminURL + "/v1", "apiKey": "ci-unused"},
				"models":  map[string]any{target.routerModel: map[string]any{"name": target.routerModel}},
			},
		},
		"model":      "llm-router/" + target.routerModel,
		"permission": map[string]any{"*": "allow"},
	}
	encoded, err := json.Marshal(configuration)
	if err != nil {
		return nil, err
	}
	return []string{
		"OPENCODE_CONFIG_CONTENT=" + string(encoded),
		"OPENCODE_DISABLE_AUTOUPDATE=1",
		"OPENCODE_DISABLE_CLAUDE_CODE=1",
	}, nil
}

func (opencodeDriver) initialArguments(turn turnRequest) []string {
	return []string{"run", "--format", "json", "-m", "llm-router/" + turn.routerModel, turn.prompt}
}

func (opencodeDriver) resumeArguments(turn turnRequest) []string {
	return []string{"run", "--format", "json", "-m", "llm-router/" + turn.routerModel, "-s", turn.sessionID, turn.prompt}
}

// opencodeEvent is one JSONL line of `opencode run --format json`.
type opencodeEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionID"`
	Part      struct {
		Type   string `json:"type"`
		Text   string `json:"text"`
		Reason string `json:"reason"`
		Tool   string `json:"tool"`
		State  struct {
			Status string `json:"status"`
		} `json:"state"`
	} `json:"part"`
}

func (opencodeDriver) parseTurn(stdout []byte, expectedSessionID string) (turnOutcome, error) {
	var sessionID, finalText, finishReason string
	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event opencodeEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return turnOutcome{}, fmt.Errorf("decode opencode event: %w", err)
		}
		if event.SessionID != "" {
			sessionID = event.SessionID
		}
		switch {
		case event.Type == "text":
			finalText = event.Part.Text
		case event.Type == "tool_use" && event.Part.State.Status == "error":
			return turnOutcome{}, fmt.Errorf("opencode tool %s failed", event.Part.Tool)
		case event.Type == "step_finish":
			finishReason = event.Part.Reason
		}
	}
	if err := scanner.Err(); err != nil {
		return turnOutcome{}, err
	}
	if finishReason != "stop" {
		return turnOutcome{}, fmt.Errorf("opencode step finished with reason %q, want stop", finishReason)
	}
	if sessionID == "" {
		return turnOutcome{}, fmt.Errorf("opencode reported no session id")
	}
	if expectedSessionID != "" && sessionID != expectedSessionID {
		return turnOutcome{}, fmt.Errorf("opencode session %q, want %q", sessionID, expectedSessionID)
	}
	return turnOutcome{sessionID: sessionID, finalText: finalText}, nil
}
