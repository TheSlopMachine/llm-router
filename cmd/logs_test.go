package cmd

import (
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/services/events"
)

func TestFormatEntry(t *testing.T) {
	value := "providers"
	entry := events.Entry{
		ID:        7,
		Timestamp: time.Date(2026, 10, 21, 9, 5, 3, 12_000_000, time.UTC),
		Severity:  2,
		Message:   "provider disabled",
		Keys:      []events.KeyValue{{Key: "subsystem", Value: &value}, {Key: "token_id"}},
	}
	want := "[WARN][21.10.2026 09:05:03.012][subsystem: providers][token_id: ] provider disabled"
	if got := formatEntry(entry); got != want {
		t.Errorf("formatEntry = %q, want %q", got, want)
	}
}

func TestSeverityNameUnknown(t *testing.T) {
	if got := severityName(9); got != "9" {
		t.Errorf("severityName(9) = %q, want 9", got)
	}
}

func TestSortedAscendingAndNewerThan(t *testing.T) {
	entries := []events.Entry{{ID: 9}, {ID: 7}, {ID: 8}}
	ordered := sortedAscending(entries)
	if ordered[0].ID != 7 || ordered[1].ID != 8 || ordered[2].ID != 9 {
		t.Errorf("sortedAscending = %v", ordered)
	}
	if entries[0].ID != 9 {
		t.Error("sortedAscending reordered its input")
	}
	newer := newerThan(entries, 7)
	if len(newer) != 2 || newer[0].ID != 8 || newer[1].ID != 9 {
		t.Errorf("newerThan = %v", newer)
	}
	if got := newerThan(entries, 9); len(got) != 0 {
		t.Errorf("newerThan at the newest id = %v, want none", got)
	}
}
