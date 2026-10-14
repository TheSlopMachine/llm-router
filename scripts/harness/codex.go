package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// codexDriver runs `codex exec`. Codex 0.162 accepts only the Responses wire
// API, so the provider points at the router's /v1/responses. Sandbox and
// approval policy live in config because `exec resume` rejects -s. The
// sandbox is danger-full-access: the workspace-write bubblewrap sandbox
// cannot set up loopback in CI containers, so every tool call fails before
// the command runs. The run workdir is already an isolated temp directory.
type codexDriver struct{}

func (codexDriver) name() string         { return "codex" }
func (codexDriver) binary() string       { return "codex" }
func (codexDriver) bareModel() string    { return "codex-harness" }
func (codexDriver) newSessionID() string { return "" }

func (codexDriver) prepare(layout runLayout, target routerTarget) ([]string, error) {
	configuration := fmt.Sprintf(`model = %q
model_provider = "llm-router"
sandbox_mode = "danger-full-access"
approval_policy = "never"

[model_providers.llm-router]
name = "llm-router"
base_url = %q
env_key = "LLM_ROUTER_API_KEY"
wire_api = "responses"
`, target.routerModel, target.adminURL+"/v1")
	if err := os.WriteFile(filepath.Join(layout.codexHome, "config.toml"), []byte(configuration), 0o644); err != nil {
		return nil, err
	}
	return []string{
		"CODEX_HOME=" + layout.codexHome,
		"LLM_ROUTER_API_KEY=ci-unused",
	}, nil
}

func (codexDriver) initialArguments(turn turnRequest) []string {
	return []string{"exec", "--skip-git-repo-check", "--json", turn.prompt}
}

func (codexDriver) resumeArguments(turn turnRequest) []string {
	return []string{"exec", "resume", turn.sessionID, "--skip-git-repo-check", "--json", turn.prompt}
}

// codexEvent is one JSONL line of `codex exec --json`.
type codexEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Item     struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
}

func (codexDriver) parseTurn(stdout []byte, expectedSessionID string) (turnOutcome, error) {
	var threadID, finalText string
	completed := false
	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event codexEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return turnOutcome{}, fmt.Errorf("decode codex event: %w", err)
		}
		switch event.Type {
		case "thread.started":
			threadID = event.ThreadID
		case "item.completed":
			// Error items are harness warnings such as missing model metadata,
			// so only agent messages carry the answer.
			if event.Item.Type == "agent_message" {
				finalText = event.Item.Text
			}
		case "turn.completed":
			completed = true
		case "turn.failed", "error":
			return turnOutcome{}, fmt.Errorf("codex reported %s: %s", event.Type, tailText(line, 400))
		}
	}
	if err := scanner.Err(); err != nil {
		return turnOutcome{}, err
	}
	if !completed {
		return turnOutcome{}, fmt.Errorf("codex turn did not complete")
	}
	if threadID == "" {
		return turnOutcome{}, fmt.Errorf("codex reported no thread id")
	}
	if expectedSessionID != "" && threadID != expectedSessionID {
		return turnOutcome{}, fmt.Errorf("codex thread %q, want %q", threadID, expectedSessionID)
	}
	return turnOutcome{sessionID: threadID, finalText: finalText}, nil
}
