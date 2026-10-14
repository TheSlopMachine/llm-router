package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// pluginFile is the mock plugin the driver expects, relative to this package.
var pluginFile = filepath.Join("..", "..", "test", "harness", "plugins", "harness-mock.lua")

func readPlugin(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(pluginFile)
	if err != nil {
		t.Fatalf("plugin %s: %v", pluginFile, err)
	}
	return string(raw)
}

// TestPhaseReplyTokensMatchPlugin keeps the driver's reply tokens equal to the
// plugin's, so a reworded plugin reply fails the driver test before any run.
func TestPhaseReplyTokensMatchPlugin(t *testing.T) {
	matches := regexp.MustCompile(`reply = "([^"]+)"`).FindAllStringSubmatch(readPlugin(t), -1)
	var fromPlugin []string
	for _, match := range matches {
		fromPlugin = append(fromPlugin, match[1])
	}
	var fromDriver []string
	for _, reply := range phaseReplies {
		fromDriver = append(fromDriver, reply)
	}
	sort.Strings(fromPlugin)
	sort.Strings(fromDriver)
	if !reflect.DeepEqual(fromPlugin, fromDriver) {
		t.Fatalf("reply tokens: plugin %v, driver %v", fromPlugin, fromDriver)
	}
}

func TestEventMessagesMatchPlugin(t *testing.T) {
	source := readPlugin(t)
	for _, message := range []string{callMessage, completeMessage} {
		if !strings.Contains(source, `"`+message+`"`) {
			t.Fatalf("plugin does not log %q", message)
		}
	}
}

func TestPluginScenarioWritesTheMarkerFiles(t *testing.T) {
	source := readPlugin(t)
	for name := range markerFiles {
		if !strings.Contains(source, name) {
			t.Fatalf("plugin scenario does not touch %s", name)
		}
	}
}

func TestPhasePromptCarriesMarkerAndRun(t *testing.T) {
	prompt := phasePrompt(2, "abc123")
	if !strings.HasPrefix(prompt, "PHASE 2 run=abc123:") {
		t.Fatalf("prompt: %q", prompt)
	}
}

func healthyEntries(nonce string) []eventEntry {
	entry := func(id int64, message string, pairs ...string) eventEntry {
		keys := []eventKeyValue{}
		for index := 0; index+1 < len(pairs); index += 2 {
			value := pairs[index+1]
			keys = append(keys, eventKeyValue{Key: pairs[index], Value: &value})
		}
		return eventEntry{ID: id, Severity: 1, Message: message, Keys: keys}
	}
	return []eventEntry{
		entry(1, callMessage, "phase", "1", "step", "call", "messages", "1", "run", nonce),
		entry(2, completeMessage, "phase", "1", "step", "complete", "messages", "3", "run", nonce),
		entry(3, callMessage, "phase", "2", "step", "call", "messages", "5", "run", nonce),
		entry(4, completeMessage, "phase", "2", "step", "complete", "messages", "7", "run", nonce),
		entry(5, callMessage, "phase", "3", "step", "call", "messages", "9", "run", nonce),
		entry(6, completeMessage, "phase", "3", "step", "complete", "messages", "11", "run", nonce),
	}
}

func TestCheckEventSequenceAcceptsHealthyRun(t *testing.T) {
	summary, err := checkEventSequence(healthyEntries("abc123"))
	if err != nil {
		t.Fatalf("healthy run rejected: %v", err)
	}
	if !strings.Contains(summary, "in order") {
		t.Fatalf("summary: %q", summary)
	}
}

func TestCheckEventSequenceRejectsErrorEvent(t *testing.T) {
	entries := healthyEntries("abc123")
	entries[3].Severity = severityError
	if _, err := checkEventSequence(entries); err == nil || !strings.Contains(err.Error(), "error event") {
		t.Fatalf("error event must fail, got %v", err)
	}
}

func TestCheckEventSequenceRejectsMissingPhase(t *testing.T) {
	entries := healthyEntries("abc123")[:4]
	if _, err := checkEventSequence(entries); err == nil {
		t.Fatalf("a run without phase 3 must fail")
	}
}

func TestCheckEventSequenceRejectsFlatHistory(t *testing.T) {
	entries := healthyEntries("abc123")
	value := "3"
	entries[4].Keys[2].Value = &value
	if _, err := checkEventSequence(entries); err == nil || !strings.Contains(err.Error(), "history") {
		t.Fatalf("a flat history across phases must fail, got %v", err)
	}
}

func TestCheckEventSequenceSortsByID(t *testing.T) {
	entries := healthyEntries("abc123")
	entries[0], entries[5] = entries[5], entries[0]
	if _, err := checkEventSequence(entries); err != nil {
		t.Fatalf("order of arrival must not matter: %v", err)
	}
}

// fakeAdmin serves the discovery endpoints the driver calls.
func fakeAdmin(t *testing.T, models []map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/llm-router/dashboard/providers", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, []map[string]any{
			{"id": "prov-openai", "type_key": "openai"},
			{"id": "prov-mock", "type_key": harnessMockType},
		})
	})
	mux.HandleFunc("/api/llm-router/dashboard/providers/prov-mock/models", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, models)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}

func TestDiscoverTargetFindsEnabledModel(t *testing.T) {
	server := fakeAdmin(t, []map[string]any{
		{"name": "claude-sonnet-4-5", "disabled": false},
		{"name": "codex-harness", "disabled": false},
	})
	target, err := discoverTarget(server.URL, "claude-sonnet-4-5")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if target.providerID != "prov-mock" || target.routerModel != "prov-mock/claude-sonnet-4-5" || target.adminURL != server.URL {
		t.Fatalf("target: %+v", target)
	}
}

func TestDiscoverTargetRejectsDisabledOrMissingModel(t *testing.T) {
	server := fakeAdmin(t, []map[string]any{
		{"name": "claude-sonnet-4-5", "disabled": true},
		{"name": "codex-harness", "disabled": false},
	})
	if _, err := discoverTarget(server.URL, "claude-sonnet-4-5"); err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("disabled model must fail, got %v", err)
	}
	if _, err := discoverTarget(server.URL, "opencode-harness"); err == nil || !strings.Contains(err.Error(), "codex-harness") {
		t.Fatalf("missing model must list the declared ones, got %v", err)
	}
}

func TestDiscoverTargetNeedsTheMockPlugin(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/llm-router/dashboard/providers", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, []map[string]any{{"id": "prov-openai", "type_key": "openai"}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	if _, err := discoverTarget(server.URL, "claude-sonnet-4-5"); err == nil || !strings.Contains(err.Error(), "upload-plugins") {
		t.Fatalf("missing plugin must point at upload-plugins, got %v", err)
	}
}

func TestFetchRunEventsScopesToTheRun(t *testing.T) {
	var seenQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events", func(writer http.ResponseWriter, request *http.Request) {
		seenQuery = request.URL.RawQuery
		writeJSON(writer, healthyEntries("abc123"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	entries, err := fetchRunEvents(server.URL, "abc123")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(entries) != 6 {
		t.Fatalf("entries: %d", len(entries))
	}
	for _, fragment := range []string{"filter.type=harness-mock", "filter.run=abc123", "limit=500"} {
		if !strings.Contains(seenQuery, fragment) {
			t.Fatalf("query %q lacks %s", seenQuery, fragment)
		}
	}
}

func TestDriverNamedRejectsUnknownHarness(t *testing.T) {
	if _, err := driverNamed("cursor"); err == nil {
		t.Fatalf("unknown harness must fail")
	}
	for _, name := range []string{"claude", "codex", "opencode"} {
		driver, err := driverNamed(name)
		if err != nil || driver.name() != name {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestNewUUIDIsVersionFour(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for index := 0; index < 20; index++ {
		if identifier := newUUID(); !pattern.MatchString(identifier) {
			t.Fatalf("not a v4 uuid: %s", identifier)
		}
	}
}

func testLayout(t *testing.T) runLayout {
	t.Helper()
	layout, err := newRunLayout(t.TempDir(), "codex", "abc123")
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	return layout
}

func TestCodexPrepareWritesResponsesProviderConfig(t *testing.T) {
	layout := testLayout(t)
	target := routerTarget{adminURL: "http://127.0.0.1:38082", routerModel: "prov-mock/codex-harness"}
	environment, err := codexDriver{}.prepare(layout, target)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(layout.codexHome, "config.toml"))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	for _, fragment := range []string{
		`model = "prov-mock/codex-harness"`,
		`wire_api = "responses"`,
		`base_url = "http://127.0.0.1:38082/v1"`,
		`sandbox_mode = "danger-full-access"`,
		`approval_policy = "never"`,
	} {
		if !strings.Contains(string(raw), fragment) {
			t.Fatalf("config lacks %s:\n%s", fragment, raw)
		}
	}
	if !contains(environment, "CODEX_HOME="+layout.codexHome) {
		t.Fatalf("environment: %v", environment)
	}
}

func TestOpenCodePrepareInlinesProviderConfig(t *testing.T) {
	layout := testLayout(t)
	target := routerTarget{adminURL: "http://127.0.0.1:38082", routerModel: "prov-mock/opencode-harness"}
	environment, err := opencodeDriver{}.prepare(layout, target)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	var inline string
	for _, entry := range environment {
		if strings.HasPrefix(entry, "OPENCODE_CONFIG_CONTENT=") {
			inline = strings.TrimPrefix(entry, "OPENCODE_CONFIG_CONTENT=")
		}
	}
	var configuration struct {
		Provider map[string]struct {
			Options struct {
				BaseURL string `json:"baseURL"`
			} `json:"options"`
			Models map[string]json.RawMessage `json:"models"`
		} `json:"provider"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal([]byte(inline), &configuration); err != nil {
		t.Fatalf("inline config: %v", err)
	}
	provider := configuration.Provider["llm-router"]
	if provider.Options.BaseURL != "http://127.0.0.1:38082/v1" {
		t.Fatalf("baseURL: %q", provider.Options.BaseURL)
	}
	if _, ok := provider.Models["prov-mock/opencode-harness"]; !ok || configuration.Model != "llm-router/prov-mock/opencode-harness" {
		t.Fatalf("model wiring: %+v", configuration)
	}
}

func TestClaudePrepareUsesAdminRootWithoutVersionPath(t *testing.T) {
	layout := testLayout(t)
	target := routerTarget{adminURL: "http://127.0.0.1:38082", routerModel: "prov-mock/claude-sonnet-4-5"}
	environment, err := claudeDriver{}.prepare(layout, target)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !contains(environment, "ANTHROPIC_BASE_URL=http://127.0.0.1:38082") {
		t.Fatalf("Claude appends /v1/messages itself, got %v", environment)
	}
	if !contains(environment, "CLAUDE_CONFIG_DIR="+layout.claudeConfig) {
		t.Fatalf("config dir not isolated: %v", environment)
	}
}

func TestClaudeArgumentsPinTheSessionAndAllowTheTools(t *testing.T) {
	turn := turnRequest{prompt: "PHASE 1 run=abc123: go", sessionID: "11111111-1111-4111-8111-111111111111", routerModel: "prov-mock/claude-sonnet-4-5"}
	initial := strings.Join(claudeDriver{}.initialArguments(turn), " ")
	if !strings.Contains(initial, "--session-id 11111111-1111-4111-8111-111111111111") || !strings.Contains(initial, "--allowedTools Bash Read Write") {
		t.Fatalf("initial arguments: %s", initial)
	}
	resume := strings.Join(claudeDriver{}.resumeArguments(turn), " ")
	if !strings.Contains(resume, "--resume 11111111-1111-4111-8111-111111111111") {
		t.Fatalf("resume arguments: %s", resume)
	}
}

func TestOpenCodeArgumentsContinueTheSession(t *testing.T) {
	turn := turnRequest{prompt: "PHASE 2 run=abc123: go", sessionID: "ses_1", routerModel: "prov-mock/opencode-harness", workdir: "/tmp/run-opencode-abc/workdir"}
	resume := strings.Join(opencodeDriver{}.resumeArguments(turn), " ")
	if !strings.Contains(resume, "-s ses_1") || !strings.Contains(resume, "-m llm-router/prov-mock/opencode-harness") {
		t.Fatalf("resume arguments: %s", resume)
	}
	if !strings.Contains(resume, "--dir /tmp/run-opencode-abc/workdir") {
		t.Fatalf("resume arguments pin no workdir: %s", resume)
	}
	initial := strings.Join(opencodeDriver{}.initialArguments(turn), " ")
	if !strings.Contains(initial, "--dir /tmp/run-opencode-abc/workdir") {
		t.Fatalf("initial arguments pin no workdir: %s", initial)
	}
}

func TestCodexArgumentsResumeTheThread(t *testing.T) {
	turn := turnRequest{prompt: "PHASE 3 run=abc123: go", sessionID: "thread-1"}
	resume := strings.Join(codexDriver{}.resumeArguments(turn), " ")
	if !strings.HasPrefix(resume, "exec resume thread-1") || !strings.Contains(resume, "--skip-git-repo-check") {
		t.Fatalf("resume arguments: %s", resume)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
