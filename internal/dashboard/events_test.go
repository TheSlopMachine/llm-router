package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/requestsource"
	"github.com/TheSlopMachine/llm-router/internal/services/events"
)

func TestEventsEndpointRequiresAuthOnPublic(t *testing.T) {
	handler, err := New(buildWiredParams(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mux := http.NewServeMux()
	handler.RegisterAPI(mux)
	request := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("public status = %d, want 401", recorder.Code)
	}
}

func TestEventsEndpointServesFiltersOnPrivate(t *testing.T) {
	parameters := buildWiredParams(t)
	handler, err := New(parameters)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	if err := parameters.EventsSvc.Append(ctx, now, events.SeverityInfo, "first", map[string]string{"plugin": "x", "provider": "openai"}); err != nil {
		t.Fatalf("append first: %v", err)
	}
	if err := parameters.EventsSvc.Append(ctx, now, events.SeverityError, "second", map[string]string{"provider": "other"}); err != nil {
		t.Fatalf("append second: %v", err)
	}
	mux := http.NewServeMux()
	handler.RegisterAPI(mux)
	serve := func(target string) []events.Entry {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request = request.WithContext(requestsource.ContextWithSource(request.Context(), requestsource.RequestSourcePrivate))
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d (%s), want 200", target, recorder.Code, recorder.Body.String())
		}
		var entries []events.Entry
		if err := json.Unmarshal(recorder.Body.Bytes(), &entries); err != nil {
			t.Fatalf("decode %s: %v", target, err)
		}
		return entries
	}
	if entries := serve("/api/events"); len(entries) != 2 {
		t.Fatalf("parameterless holds %d rows, want 2", len(entries))
	}
	if entries := serve("/api/events?severity=3"); len(entries) != 1 || entries[0].Message != "second" {
		t.Fatalf("severity filter holds %+v, want second only", entries)
	}
	if entries := serve("/api/events?filter.plugin&filter.provider=openai"); len(entries) != 1 || entries[0].Message != "first" {
		t.Fatalf("key filters hold %+v, want first only", entries)
	}
	if entries := serve("/api/events?exclude.plugin"); len(entries) != 1 || entries[0].Message != "second" {
		t.Fatalf("key exclusions hold %+v, want second only", entries)
	}
	if entries := serve("/api/events?filter.provider&exclude.provider=openai"); len(entries) != 1 || entries[0].Message != "second" {
		t.Fatalf("combined filters hold %+v, want second only", entries)
	}
}

func TestEventsEndpointRejectsInvalidParams(t *testing.T) {
	handler, err := New(buildWiredParams(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mux := http.NewServeMux()
	handler.RegisterAPI(mux)
	for _, target := range []string{
		"/api/events?severity=bogus",
		"/api/events?severity=9",
		"/api/events?id=bogus",
		"/api/events?start=bogus",
		"/api/events?end=bogus",
		"/api/events?limit=bogus",
		"/api/events?filter.",
		"/api/events?exclude.",
	} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request = request.WithContext(requestsource.ContextWithSource(request.Context(), requestsource.RequestSourcePrivate))
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("GET %s: status = %d, want 400", target, recorder.Code)
		}
	}
}
