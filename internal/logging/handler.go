// Package logging provides the bracket log handler for llm-router:
// [LEVEL][dd.mm.yyyy h:m:s.ms][key: value] message.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// TimeFormat renders record timestamps as dd.mm.yyyy h:m:s.ms.
const TimeFormat = "02.01.2006 15:04:05.000"

// Handler formats records as bracket groups. Each attribute renders as its
// own [key: value] group in call order. Groups flatten with dot separators.
type Handler struct {
	level      slog.Level
	writer     io.Writer
	mu         *sync.Mutex
	attributes []slog.Attr
	groups     []string
}

// NewHandler builds a Handler writing to writer at level.
func NewHandler(writer io.Writer, level slog.Level) *Handler {
	return &Handler{writer: writer, level: level, mu: &sync.Mutex{}}
}

// Enabled reports whether level passes the configured threshold.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// WithAttrs returns a Handler with attributes appended, qualified by groups.
func (h *Handler) WithAttrs(attributes []slog.Attr) slog.Handler {
	cloned := &Handler{writer: h.writer, level: h.level, mu: h.mu}
	cloned.groups = append([]string(nil), h.groups...)
	cloned.attributes = append(append([]slog.Attr(nil), h.attributes...), qualify(h.groups, attributes)...)
	return cloned
}

// WithGroup returns a Handler scoping subsequent attributes under name.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	cloned := &Handler{writer: h.writer, level: h.level, mu: h.mu}
	cloned.groups = append(append([]string(nil), h.groups...), name)
	cloned.attributes = append([]slog.Attr(nil), h.attributes...)
	return cloned
}

// Handle renders one record in bracket format.
func (h *Handler) Handle(_ context.Context, record slog.Record) error {
	level := strings.ToUpper(record.Level.String())
	timestamp := record.Time
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
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
		builder.WriteString(redactLogText(record.Message))
	}
	builder.WriteString("\n")
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.writer, builder.String())
	return err
}

func recordAttrs(record slog.Record) []slog.Attr {
	var output []slog.Attr
	record.Attrs(func(attribute slog.Attr) bool {
		output = append(output, attribute)
		return true
	})
	return output
}

// qualify flattens attributes, prefixing keys with groups and expanding groups.
func qualify(groups []string, attributes []slog.Attr) []slog.Attr {
	prefix := strings.Join(groups, ".")
	var output []slog.Attr
	for _, attribute := range attributes {
		output = append(output, flatten(prefix, attribute)...)
	}
	return output
}

func flatten(prefix string, attribute slog.Attr) []slog.Attr {
	attribute.Value = attribute.Value.Resolve()
	if attribute.Value.Kind() == slog.KindGroup {
		name := attribute.Key
		if prefix != "" && name != "" {
			name = prefix + "." + name
		} else if name == "" {
			name = prefix
		}
		var output []slog.Attr
		for _, sub := range attribute.Value.Group() {
			output = append(output, flatten(name, sub)...)
		}
		return output
	}
	key := attribute.Key
	if key == "" {
		if prefix == "" {
			return nil
		}
		key = prefix
	} else if prefix != "" {
		key = prefix + "." + key
	}
	return []slog.Attr{slog.String(key, redactLogValue(key, formatValue(attribute.Value)))}
}

func writeFlat(builder *strings.Builder, attribute slog.Attr) {
	if attribute.Key == "" {
		return
	}
	builder.WriteString("[")
	builder.WriteString(attribute.Key)
	builder.WriteString(": ")
	builder.WriteString(attribute.Value.String())
	builder.WriteString("]")
}

func formatValue(value slog.Value) string {
	value = value.Resolve()
	switch value.Kind() {
	case slog.KindString:
		return value.String()
	case slog.KindInt64:
		return fmt.Sprintf("%d", value.Int64())
	case slog.KindUint64:
		return fmt.Sprintf("%d", value.Uint64())
	case slog.KindFloat64:
		return fmt.Sprintf("%v", value.Float64())
	case slog.KindBool:
		return fmt.Sprintf("%v", value.Bool())
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindTime:
		return value.Time().Format(TimeFormat)
	case slog.KindGroup:
		return fmt.Sprintf("%v", value.Any())
	default:
		if err, ok := value.Any().(error); ok && err != nil {
			return err.Error()
		}
		return fmt.Sprintf("%v", value.Any())
	}
}
