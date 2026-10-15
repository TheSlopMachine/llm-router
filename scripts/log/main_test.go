package main

import (
	"testing"
)

func TestBuildQueryExcludes(t *testing.T) {
	t.Setenv("EVENTS_SEVERITY", "")
	t.Setenv("EVENTS_ID", "")
	t.Setenv("EVENTS_START", "")
	t.Setenv("EVENTS_END", "")
	t.Setenv("EVENTS_LIMIT", "50")
	t.Setenv("LINES", "")
	t.Setenv("EVENT_FILTERS", "type=google !event=stream !debug code=rate_limit")
	query := buildQuery()
	if got := query.Get("filter.type"); got != "google" {
		t.Fatalf("filter.type = %q, want google", got)
	}
	if got := query.Get("filter.code"); got != "rate_limit" {
		t.Fatalf("filter.code = %q, want rate_limit", got)
	}
	if got := query.Get("exclude.event"); got != "stream" {
		t.Fatalf("exclude.event = %q, want stream", got)
	}
	if values, found := query["exclude.debug"]; !found || len(values) != 1 {
		t.Fatalf("exclude.debug missing, query = %v", query)
	}
	if _, found := query["filter.debug"]; found {
		t.Fatalf("filter.debug present, query = %v", query)
	}
}

func TestOrderEntriesAscending(t *testing.T) {
	entries := []entry{{ID: 3}, {ID: 2}, {ID: 1}}
	ordered := orderEntriesAscending(entries)
	for position, identifier := range []int64{1, 2, 3} {
		if ordered[position].ID != identifier {
			t.Fatalf("position %d holds ID %d, want %d", position, ordered[position].ID, identifier)
		}
	}
	if entries[0].ID != 3 {
		t.Fatalf("input mutated, first ID = %d, want 3", entries[0].ID)
	}
}

func TestSelectNewEntriesKeepsBurstInOrder(t *testing.T) {
	entries := []entry{{ID: 5}, {ID: 4}, {ID: 3}}
	selected := selectNewEntries(entries, 3)
	if len(selected) != 2 || selected[0].ID != 4 || selected[1].ID != 5 {
		t.Fatalf("selected holds %+v, want IDs 4 then 5", selected)
	}
	if len(selectNewEntries(entries, 5)) != 0 {
		t.Fatalf("expected no rows newer than 5")
	}
}
