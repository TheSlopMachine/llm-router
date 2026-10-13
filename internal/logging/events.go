// Package logging provides the bracket log handler for llm-router plus the
// events handler that persists records at or above the configured level
// to the events database while rendering them to stderr.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/services/events"
)

// EventsQueueSize bounds pending event writes. Sends block when full so
// no event is dropped; producers wait for the writer.
const EventsQueueSize = 4096

type pendingEvent struct {
	timestamp time.Time
	severity  int
	message   string
	keys      map[string]string
}

// EventsHandler persists records at or above the level to the events
// database and renders them to stderr in bracket format. Records below
// the level are dropped: never persisted, never rendered.
type EventsHandler struct {
	level      slog.Level
	writer     io.Writer
	writerMu   *sync.Mutex
	attributes []slog.Attr
	groups     []string

	service *events.Service
	queue   chan pendingEvent
}

// NewEventsHandler builds an EventsHandler writing display lines to writer
// at level and persisting through service. It starts the writer goroutine.
func NewEventsHandler(writer io.Writer, level slog.Level, service *events.Service) *EventsHandler {
	handler := &EventsHandler{
		level:    level,
		writer:   writer,
		writerMu: &sync.Mutex{},
		service:  service,
		queue:    make(chan pendingEvent, EventsQueueSize),
	}
	go handler.run()
	return handler
}

// SetService wires the persistence target after construction. Startup
// creates the handler before the events database opens; the server sets
// the service once it exists. Display never waits for this.
func (h *EventsHandler) SetService(service *events.Service) {
	h.service = service
}

// Drain waits until the pending queue empties or timeout elapses,
// reporting whether the queue drained. Shutdown calls it after the
// listeners stop so the final lines persist before the events file
// vacuums. The writer keeps running; Drain only observes.
func (h *EventsHandler) Drain(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for len(h.queue) > 0 {
		if time.Now().After(deadline) {
			observed := slog.NewRecord(time.Now(), slog.LevelWarn, "Event drain times out", 0)
			observed.Add(slog.Int("count", len(h.queue)))
			_ = h.render(observed, time.Now())
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return true
}

// Enabled reports whether the record meets the persistence threshold.
// slog filters before Handle, so records below the level never reach it.
func (h *EventsHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// WithAttrs returns a Handler with attributes appended, qualified by groups.
func (h *EventsHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	cloned := &EventsHandler{
		level:    h.level,
		writer:   h.writer,
		writerMu: h.writerMu,
		service:  h.service,
		queue:    h.queue,
	}
	cloned.groups = append([]string(nil), h.groups...)
	cloned.attributes = append(append([]slog.Attr(nil), h.attributes...), qualify(h.groups, attributes)...)
	return cloned
}

// WithGroup returns a Handler scoping subsequent attributes under name.
func (h *EventsHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	cloned := &EventsHandler{
		level:    h.level,
		writer:   h.writer,
		writerMu: h.writerMu,
		service:  h.service,
		queue:    h.queue,
	}
	cloned.groups = append(append([]string(nil), h.groups...), name)
	cloned.attributes = append([]slog.Attr(nil), h.attributes...)
	return cloned
}

// Handle drops records below the level, then renders the display line
// and enqueues the record for persistence. The send blocks when the
// queue is full.
func (h *EventsHandler) Handle(_ context.Context, record slog.Record) error {
	if record.Level < h.level {
		return nil
	}
	timestamp := record.Time
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	severity := severityForLevel(record.Level)
	keys := map[string]string{}
	for _, attribute := range h.attributes {
		if attribute.Key != "" {
			keys[attribute.Key] = attribute.Value.String()
		}
	}
	for _, attribute := range qualify(h.groups, recordAttrs(record)) {
		if attribute.Key != "" {
			keys[attribute.Key] = attribute.Value.String()
		}
	}
	if err := h.render(record, timestamp); err != nil {
		return err
	}
	if h.service == nil || h.queue == nil {
		return nil
	}
	if cap(h.queue) > 0 && len(h.queue) >= cap(h.queue) {
		observed := slog.NewRecord(time.Now(), slog.LevelWarn, "Event queue is full", 0)
		observed.Add(slog.Int("count", len(h.queue)))
		_ = h.render(observed, time.Now())
	}
	h.queue <- pendingEvent{timestamp: timestamp, severity: severity, message: record.Message, keys: keys}
	return nil
}

// Fatal persists a fatal event synchronously, renders it, then exits.
func (h *EventsHandler) Fatal(message string, attributes ...any) {
	timestamp := time.Now()
	keys := map[string]string{}
	for _, attribute := range h.attributes {
		if attribute.Key != "" {
			keys[attribute.Key] = attribute.Value.String()
		}
	}
	record := slog.NewRecord(timestamp, slog.LevelError, message, 0)
	record.Add(attributes...)
	for _, attribute := range qualify(h.groups, recordAttrs(record)) {
		if attribute.Key != "" {
			keys[attribute.Key] = attribute.Value.String()
		}
	}
	_ = h.render(record, timestamp)
	if h.service != nil {
		_ = h.service.Append(context.Background(), timestamp, events.SeverityFatal, message, keys)
	}
	os.Exit(1)
}

func (h *EventsHandler) run() {
	for event := range h.queue {
		if h.service == nil {
			continue
		}
		_ = h.service.Append(context.Background(), event.timestamp, event.severity, event.message, event.keys)
	}
}

func (h *EventsHandler) render(record slog.Record, timestamp time.Time) error {
	level := strings.ToUpper(record.Level.String())
	var builder strings.Builder
	builder.WriteString("[")
	builder.WriteString(level)
	builder.WriteString("][")
	builder.WriteString(timestamp.Format(TimeFormat))
	builder.WriteString("]")
	for _, attribute := range h.attributes {
		writeFlat(&builder, attribute)
	}
	for _, attribute := range qualify(h.groups, recordAttrs(record)) {
		writeFlat(&builder, attribute)
	}
	if record.Message != "" {
		builder.WriteString(" ")
		builder.WriteString(record.Message)
	}
	builder.WriteString("\n")
	h.writerMu.Lock()
	defer h.writerMu.Unlock()
	_, err := io.WriteString(h.writer, builder.String())
	return err
}

func severityForLevel(level slog.Level) int {
	switch {
	case level < slog.LevelInfo:
		return events.SeverityDebug
	case level < slog.LevelWarn:
		return events.SeverityInfo
	case level < slog.LevelError:
		return events.SeverityWarn
	default:
		return events.SeverityError
	}
}
