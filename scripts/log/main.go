package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

// log queries the running dev stack for persisted events and prints them.
// Query parameters come from env: EVENTS_SEVERITY, EVENTS_ID, EVENTS_START,
// EVENTS_END, EVENTS_LIMIT (LINES fallback), EVENT_FILTERS="k=v bare-key".
// FOLLOW=1 polls for new rows; FOLLOW=0 dumps once.
func main() {
	base := adminBase()
	query := buildQuery()
	entries, err := fetch(base, query)
	if err != nil {
		shared.Failf("%v", err)
	}
	lastMax := printEntries(entries)
	if !shared.FollowFromEnv() {
		return
	}
	for {
		time.Sleep(time.Second)
		entries, err := fetch(base, query)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.ID > lastMax {
				fmt.Println(formatEntry(entry))
				lastMax = entry.ID
			}
		}
	}
}

type keyValue struct {
	Key   string  `json:"key"`
	Value *string `json:"value"`
}

type entry struct {
	ID        int64      `json:"id"`
	Timestamp time.Time  `json:"timestamp"`
	Severity  int        `json:"severity"`
	Message   string     `json:"message"`
	Keys      []keyValue `json:"keys"`
}

func adminBase() string {
	if port := strings.TrimSpace(os.Getenv("ADMIN_PORT")); port != "" {
		return "http://127.0.0.1:" + port
	}
	pidFile := shared.Getenv("PID_FILE", shared.DefaultPidFile())
	if params, err := shared.ReadStartParams(pidFile); err == nil && strings.TrimSpace(params.AdminPort) != "" {
		return "http://127.0.0.1:" + strings.TrimSpace(params.AdminPort)
	}
	return "http://127.0.0.1:" + shared.DevAdminPort
}

func buildQuery() url.Values {
	query := url.Values{}
	if raw := strings.TrimSpace(os.Getenv("EVENTS_SEVERITY")); raw != "" {
		query.Set("severity", raw)
	}
	if raw := strings.TrimSpace(os.Getenv("EVENTS_ID")); raw != "" {
		query.Set("id", raw)
	}
	if raw := strings.TrimSpace(os.Getenv("EVENTS_START")); raw != "" {
		query.Set("start", raw)
	}
	if raw := strings.TrimSpace(os.Getenv("EVENTS_END")); raw != "" {
		query.Set("end", raw)
	}
	limit := strings.TrimSpace(os.Getenv("EVENTS_LIMIT"))
	if limit == "" {
		if lines := shared.LinesFromEnv(0); lines > 0 {
			limit = strconv.Itoa(lines)
		} else {
			limit = "50"
		}
	}
	query.Set("limit", limit)
	for _, token := range strings.Fields(os.Getenv("EVENT_FILTERS")) {
		if token == "" {
			continue
		}
		if key, value, found := strings.Cut(token, "="); found {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			query.Set("filter."+key, strings.TrimSpace(value))
		} else {
			query.Set("filter."+strings.TrimSpace(token), "")
		}
	}
	return query
}

func fetch(base string, query url.Values) ([]entry, error) {
	target := base + "/api/events?" + query.Encode()
	response, err := http.Get(target)
	if err != nil {
		return nil, fmt.Errorf("query events %s: %w (is the dev server running? see make status)", target, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read events response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query events: status %d: %s", response.StatusCode, strings.TrimSpace(string(raw)))
	}
	var entries []entry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("decode events response: %w", err)
	}
	return entries, nil
}

func printEntries(entries []entry) int64 {
	var lastMax int64
	for _, entry := range entries {
		fmt.Println(formatEntry(entry))
		if entry.ID > lastMax {
			lastMax = entry.ID
		}
	}
	return lastMax
}

func formatEntry(entry entry) string {
	var builder strings.Builder
	builder.WriteString("[")
	builder.WriteString(severityName(entry.Severity))
	builder.WriteString("][")
	builder.WriteString(entry.Timestamp.Format("02.01.2006 15:04:05.000"))
	builder.WriteString("]")
	for _, key := range entry.Keys {
		builder.WriteString("[")
		builder.WriteString(key.Key)
		builder.WriteString(": ")
		if key.Value != nil {
			builder.WriteString(*key.Value)
		}
		builder.WriteString("]")
	}
	if entry.Message != "" {
		builder.WriteString(" ")
		builder.WriteString(entry.Message)
	}
	return builder.String()
}

func severityName(severity int) string {
	switch severity {
	case 0:
		return "DEBUG"
	case 1:
		return "INFO"
	case 2:
		return "WARN"
	case 3:
		return "ERROR"
	case 4:
		return "FATAL"
	default:
		return strconv.Itoa(severity)
	}
}
