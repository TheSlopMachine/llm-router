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
	level  slog.Level
	w      io.Writer
	mu     *sync.Mutex
	attrs  []slog.Attr
	groups []string
}

// NewHandler builds a Handler writing to w at level.
func NewHandler(w io.Writer, level slog.Level) *Handler {
	return &Handler{w: w, level: level, mu: &sync.Mutex{}}
}

// Enabled reports whether level passes the configured threshold.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// WithAttrs returns a Handler with attrs appended, qualified by groups.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	nh := &Handler{w: h.w, level: h.level, mu: h.mu}
	nh.groups = append([]string(nil), h.groups...)
	nh.attrs = append(append([]slog.Attr(nil), h.attrs...), qualify(h.groups, attrs)...)
	return nh
}

// WithGroup returns a Handler scoping subsequent attrs under name.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	nh := &Handler{w: h.w, level: h.level, mu: h.mu}
	nh.groups = append(append([]string(nil), h.groups...), name)
	nh.attrs = append([]slog.Attr(nil), h.attrs...)
	return nh
}

// Handle renders one record in bracket format.
func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	level := strings.ToUpper(r.Level.String())
	ts := r.Time
	if ts.IsZero() {
		ts = time.Now()
	}
	var b strings.Builder
	b.WriteString("[")
	b.WriteString(level)
	b.WriteString("][")
	b.WriteString(ts.Format(TimeFormat))
	b.WriteString("]")
	for _, a := range h.attrs {
		writeFlat(&b, a)
	}
	for _, a := range qualify(h.groups, recordAttrs(r)) {
		writeFlat(&b, a)
	}
	if r.Message != "" {
		b.WriteString(" ")
		b.WriteString(r.Message)
	}
	b.WriteString("\n")
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

func recordAttrs(r slog.Record) []slog.Attr {
	var out []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		out = append(out, a)
		return true
	})
	return out
}

// qualify flattens attrs, prefixing keys with groups and expanding groups.
func qualify(groups []string, attrs []slog.Attr) []slog.Attr {
	prefix := strings.Join(groups, ".")
	var out []slog.Attr
	for _, a := range attrs {
		out = append(out, flatten(prefix, a)...)
	}
	return out
}

func flatten(prefix string, a slog.Attr) []slog.Attr {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		name := a.Key
		if prefix != "" && name != "" {
			name = prefix + "." + name
		} else if name == "" {
			name = prefix
		}
		var out []slog.Attr
		for _, sub := range a.Value.Group() {
			out = append(out, flatten(name, sub)...)
		}
		return out
	}
	key := a.Key
	if key == "" {
		if prefix == "" {
			return nil
		}
		key = prefix
	} else if prefix != "" {
		key = prefix + "." + key
	}
	return []slog.Attr{slog.String(key, formatValue(a.Value))}
}

func writeFlat(b *strings.Builder, a slog.Attr) {
	if a.Key == "" {
		return
	}
	b.WriteString("[")
	b.WriteString(a.Key)
	b.WriteString(": ")
	b.WriteString(a.Value.String())
	b.WriteString("]")
}

func formatValue(v slog.Value) string {
	v = v.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return fmt.Sprintf("%d", v.Int64())
	case slog.KindUint64:
		return fmt.Sprintf("%d", v.Uint64())
	case slog.KindFloat64:
		return fmt.Sprintf("%v", v.Float64())
	case slog.KindBool:
		return fmt.Sprintf("%v", v.Bool())
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().Format(TimeFormat)
	case slog.KindGroup:
		return fmt.Sprintf("%v", v.Any())
	default:
		if err, ok := v.Any().(error); ok && err != nil {
			return err.Error()
		}
		return fmt.Sprintf("%v", v.Any())
	}
}
