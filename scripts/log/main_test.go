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
