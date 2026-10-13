package luaplugin

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/services/events"
)

func runLogSnippet(t *testing.T, service *Service, pluginID, typeKey, snippet string) {
	t.Helper()
	state := newSandboxState(&executionContext{
		pluginID: pluginID,
		typeKey:  typeKey,
		logSink:  service.appendLog,
	})
	defer state.Close()
	if err := state.DoString(snippet); err != nil {
		t.Fatalf("run snippet: %v", err)
	}
}

func pluginLogEntries(t *testing.T, service *Service, pluginID string) []LogEntry {
	t.Helper()
	entries, err := service.Logs(context.Background(), pluginID)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	return entries
}

func TestLogPersistsSeverityAndKeys(t *testing.T) {
	service := setupService(t)
	runLogSnippet(t, service, "plugin-one", "log-type", `llm_router.log("warn", "hello", { provider = "openai" })`)
	entries := pluginLogEntries(t, service, "plugin-one")
	if len(entries) != 1 {
		t.Fatalf("entries hold %d rows, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Severity != events.SeverityWarn || entry.Message != "hello" {
		t.Errorf("unexpected entry %+v", entry)
	}
	if entry.Keys["provider"] != "openai" || entry.Keys["plugin"] != "plugin-one" || entry.Keys["type"] != "log-type" {
		t.Errorf("unexpected keys %+v", entry.Keys)
	}
}

func TestPrintShimsInfoSeverity(t *testing.T) {
	service := setupService(t)
	runLogSnippet(t, service, "plugin-two", "print-type", `print("a", "b")`)
	entries := pluginLogEntries(t, service, "plugin-two")
	if len(entries) != 1 {
		t.Fatalf("entries hold %d rows, want 1", len(entries))
	}
	if entries[0].Severity != events.SeverityInfo || entries[0].Message != "a\tb" {
		t.Errorf("unexpected entry %+v", entries[0])
	}
	if entries[0].Keys["type"] != "print-type" {
		t.Errorf("unexpected keys %+v", entries[0].Keys)
	}
}

func TestAppendLogHonorsMinimumLevel(t *testing.T) {
	service := setupService(t)
	service.SetMinimumLevel(slog.LevelWarn)
	service.appendLog("plugin-four", "log-type", events.SeverityDebug, "quiet debug", nil)
	service.appendLog("plugin-four", "log-type", events.SeverityInfo, "quiet info", nil)
	service.appendLog("plugin-four", "log-type", events.SeverityWarn, "kept warn", nil)
	service.appendLog("plugin-four", "log-type", events.SeverityError, "kept error", nil)
	entries := pluginLogEntries(t, service, "plugin-four")
	if len(entries) != 2 {
		t.Fatalf("entries hold %d rows, want 2", len(entries))
	}
	for _, entry := range entries {
		if entry.Message != "kept warn" && entry.Message != "kept error" {
			t.Errorf("unexpected entry %+v", entry)
		}
	}
}

func TestLogRejectsBadInput(t *testing.T) {
	service := setupService(t)
	for _, snippet := range []string{
		`llm_router.log("verbose", "hi")`,
		`llm_router.log(42, "hi")`,
		`llm_router.log("info")`,
		`llm_router.log("info", "hi", { plugin = "spoof" })`,
		`llm_router.log("info", "hi", { type = "spoof" })`,
		`llm_router.log("info", "hi", "nope")`,
	} {
		state := newSandboxState(&executionContext{
			pluginID: "plugin-three",
			typeKey:  "log-type",
			logSink:  service.appendLog,
		})
		err := state.DoString(snippet)
		state.Close()
		if err == nil {
			t.Errorf("snippet %s succeeded, want Lua error", snippet)
			continue
		}
		if !strings.Contains(err.Error(), "llm_router.log") {
			t.Errorf("snippet %s error %v must name llm_router.log", snippet, err)
		}
	}
	if entries := pluginLogEntries(t, service, "plugin-three"); len(entries) != 0 {
		t.Errorf("rejected logs hold %d rows, want 0", len(entries))
	}
}
