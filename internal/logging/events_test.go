package logging

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/services/events"
)

func openEventsService(t *testing.T) *events.Service {
	t.Helper()
	directory := t.TempDir()
	database, err := db.OpenSQLite(
		context.Background(),
		filepath.Join(directory, "logging.sqlite3"),
		filepath.Join(directory, "logging.cache.sqlite3"),
		filepath.Join(directory, "logging.events.sqlite3"),
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	service, err := events.New(database, 3)
	if err != nil {
		t.Fatalf("new events service: %v", err)
	}
	return service
}

func TestEventsHandlerPersistsAllLevels(t *testing.T) {
	service := openEventsService(t)
	var buffer bytes.Buffer
	handler := NewEventsHandler(&buffer, slog.LevelError, service)
	logger := slog.New(handler)
	logger.Debug("debug line", "key", "debug-value")
	logger.Info("info line")
	logger.Error("error line")
	deadline := time.Now().Add(5 * time.Second)
	for {
		entries, err := service.Query(context.Background(), events.Query{Limit: 10})
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if len(entries) == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("entries hold %d rows, want 3", len(entries))
		}
		time.Sleep(10 * time.Millisecond)
	}
	display := buffer.String()
	if strings.Contains(display, "debug line") || strings.Contains(display, "info line") {
		t.Fatalf("display shows filtered lines: %q", display)
	}
	if !strings.Contains(display, "error line") {
		t.Fatalf("display misses error line: %q", display)
	}
}

func TestEventsHandlerKeepsBracketFormat(t *testing.T) {
	service := openEventsService(t)
	var buffer bytes.Buffer
	handler := NewEventsHandler(&buffer, slog.LevelDebug, service)
	record := slog.NewRecord(time.Date(2026, 9, 25, 12, 22, 15, 0, time.Local), slog.LevelWarn, "all credentials failed", 0)
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !strings.HasPrefix(buffer.String(), "[WARN][25.09.2026 12:22:15.000] all credentials failed") {
		t.Fatalf("unexpected line %q", buffer.String())
	}
}
