package main

import (
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// The plugin's log messages. The plugin owns them; TestEventMessagesMatchPlugin
// keeps both sides in step.
const (
	callMessage     = "Harness phase issues tool call"
	completeMessage = "Harness phase completes"
	severityError   = 3
)

// eventKeyValue is one metadata pair of a persisted event.
type eventKeyValue struct {
	Key   string  `json:"key"`
	Value *string `json:"value"`
}

// eventEntry is one persisted event as GET /api/events returns it.
type eventEntry struct {
	ID       int64           `json:"id"`
	Severity int             `json:"severity"`
	Message  string          `json:"message"`
	Keys     []eventKeyValue `json:"keys"`
}

// fetchRunEvents returns the events the plugin logged for one run. The run
// nonce scopes the query, so reruns against the same database stay isolated.
func fetchRunEvents(base, nonce string) ([]eventEntry, error) {
	query := url.Values{}
	query.Set("filter.type", harnessMockType)
	query.Set("filter.run", nonce)
	query.Set("limit", "500")
	var entries []eventEntry
	if err := requestJSON("GET", base+"/api/events?"+query.Encode(), nil, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// keyMap flattens the metadata pairs of one event.
func keyMap(pairs []eventKeyValue) map[string]string {
	values := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		if pair.Value != nil {
			values[pair.Key] = *pair.Value
		}
	}
	return values
}

// checkEventSequence verifies the phase events of one run, in id order: each
// phase issues one tool call and then completes, phases run 1 to 3 in order,
// the history each call sees grows, and no event is an error. It returns a
// one-line summary for the run log.
func checkEventSequence(entries []eventEntry) (string, error) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	var sequence []string
	var callMessages []int
	for _, entry := range entries {
		keys := keyMap(entry.Keys)
		if entry.Severity >= severityError {
			return "", fmt.Errorf("plugin error event %d: %s (reason %s)", entry.ID, entry.Message, keys["reason"])
		}
		switch entry.Message {
		case callMessage:
			sequence = append(sequence, keys["phase"]+":call")
			count, err := strconv.Atoi(keys["messages"])
			if err != nil {
				return "", fmt.Errorf("call event %d has no message count: %w", entry.ID, err)
			}
			callMessages = append(callMessages, count)
		case completeMessage:
			sequence = append(sequence, keys["phase"]+":complete")
		}
	}
	want := []string{"1:call", "1:complete", "2:call", "2:complete", "3:call", "3:complete"}
	if !reflect.DeepEqual(sequence, want) {
		return "", fmt.Errorf("phase events: got %s, want %s", strings.Join(sequence, " "), strings.Join(want, " "))
	}
	for index := 1; index < len(callMessages); index++ {
		if callMessages[index] <= callMessages[index-1] {
			return "", fmt.Errorf("history did not grow across phases: %v", callMessages)
		}
	}
	return fmt.Sprintf("phase events in order, history per call %v", callMessages), nil
}
