package dashboard

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/services/events"
)

// apiEvents answers GET /api/events with severity, id, time-range,
// filter.<key> predicates and exclude.<key> predicates. Built-in parameters
// live at root level; arbitrary metadata keys carry the filter. or exclude.
// prefix so collisions are impossible.
//
// @Summary      List events
// @Description  Query persisted log events by severity, time range and metadata keys.
// @Tags         events
// @Produce      json
// @Param        severity query int false "minimum severity (0-4)"
// @Param        id query int false "single event id"
// @Param        start query string false "RFC3339 start (inclusive)"
// @Param        end query string false "RFC3339 end (inclusive)"
// @Param        limit query int false "max rows (default 50, max 500)"
// @Success      200 {array} events.Entry
// @Failure      400 {object} map[string]string
// @Router       /api/events [get]
func (h *Handler) apiEvents(w http.ResponseWriter, r *http.Request) {
	if h.eventsService == nil {
		h.logError("Events unavailable")
		h.jsonErr(w, http.StatusServiceUnavailable, "events unavailable")
		return
	}
	query, err := parseEventsQuery(r)
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := h.eventsService.Query(r.Context(), query)
	if err != nil {
		h.logError("Failed to query events", "error", err)
		h.jsonErr(w, http.StatusInternalServerError, "query events failed")
		return
	}
	if entries == nil {
		entries = []events.Entry{}
	}
	h.json(w, http.StatusOK, entries)
}

func parseEventsQuery(r *http.Request) (events.Query, error) {
	values := r.URL.Query()
	var query events.Query
	if raw := strings.TrimSpace(values.Get("severity")); raw != "" {
		number, err := strconv.Atoi(raw)
		if err != nil || number < events.SeverityDebug || number > events.SeverityFatal {
			return events.Query{}, fmt.Errorf("invalid severity %q: want 0-4", raw)
		}
		query.Severity = &number
	}
	if raw := strings.TrimSpace(values.Get("id")); raw != "" {
		number, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || number < 1 {
			return events.Query{}, fmt.Errorf("invalid id %q", raw)
		}
		query.ID = &number
	}
	if raw := strings.TrimSpace(values.Get("start")); raw != "" {
		moment, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return events.Query{}, fmt.Errorf("invalid start %q: want RFC3339", raw)
		}
		query.Start = &moment
	}
	if raw := strings.TrimSpace(values.Get("end")); raw != "" {
		moment, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return events.Query{}, fmt.Errorf("invalid end %q: want RFC3339", raw)
		}
		query.End = &moment
	}
	if query.Start != nil && query.End != nil && query.Start.After(*query.End) {
		return events.Query{}, fmt.Errorf("start is after end")
	}
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		number, err := strconv.Atoi(raw)
		if err != nil || number < 1 {
			return events.Query{}, fmt.Errorf("invalid limit %q", raw)
		}
		if number > events.MaxLimit {
			number = events.MaxLimit
		}
		query.Limit = number
	}
	for key, list := range values {
		exclusion := false
		name := ""
		switch {
		case strings.HasPrefix(key, "filter."):
			name = strings.TrimSpace(strings.TrimPrefix(key, "filter."))
			if name == "" {
				return events.Query{}, fmt.Errorf("invalid filter key")
			}
		case strings.HasPrefix(key, "exclude."):
			exclusion = true
			name = strings.TrimSpace(strings.TrimPrefix(key, "exclude."))
			if name == "" {
				return events.Query{}, fmt.Errorf("invalid exclude key")
			}
		default:
			continue
		}
		if len(list) == 0 || strings.TrimSpace(list[0]) == "" {
			if exclusion {
				query.Excludes = append(query.Excludes, events.KeyFilter{Key: name})
			} else {
				query.Filters = append(query.Filters, events.KeyFilter{Key: name})
			}
			continue
		}
		value := list[0]
		if exclusion {
			query.Excludes = append(query.Excludes, events.KeyFilter{Key: name, Value: &value})
		} else {
			query.Filters = append(query.Filters, events.KeyFilter{Key: name, Value: &value})
		}
	}
	return query, nil
}
