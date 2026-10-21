package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TheSlopMachine/llm-router/internal/cli"
	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/services/events"
)

const (
	logsDefaultLimit  = 50
	logsFollowLimit   = 500
	logsFollowPeriod  = time.Second
	logsSubsystemKey  = "subsystem"
	logsTimestampForm = "02.01.2006 15:04:05.000"
)

var logsOptions struct {
	subsystem string
	severity  string
	filters   []string
	excludes  []string
	id        int64
	from      string
	to        string
	limit     int
	follow    bool
	asJSON    bool
}

func newLogsCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "logs",
		Short: "Print server events",
		Long: `Print the events the server persisted. The command reads the events
database directly, so the server does not need to run, and it works from any
terminal.

--severity is a minimum: debug, info, warn, error or fatal. The server
persists only events at or above its LOG_LEVEL, so "--severity debug" shows
nothing below that level.

--subsystem narrows to one part of the server, for example providers,
virtual_models, plugins, plugin_repos, plugin_storage, tokens, credentials,
proxy or doctor. --filter and --exclude match any other event key exactly:
"--filter code=rate_limit" matches the value, "--filter token_id" matches
events that carry the key. Both repeat. Keys are listed in docs/EVENT-KEYS.md.

--from and --to take an RFC 3339 timestamp or an age: 30m, 6h, 2d.`,
		Example: `  llm-router logs
  llm-router logs -f --subsystem providers --severity warn
  llm-router logs --from 2h --filter code=rate_limit --limit 200
  llm-router logs --json | jq .message`,
		GroupID: groupMaintain,
		Args:    cobra.NoArgs,
		RunE:    runLogs,
	}
	flags := command.Flags()
	flags.StringVar(&logsOptions.subsystem, "subsystem", "", "only events of this subsystem")
	flags.StringVar(&logsOptions.severity, "severity", "", "minimum severity: debug, info, warn, error, fatal")
	flags.StringArrayVar(&logsOptions.filters, "filter", nil, "only events with key[=value]; repeatable")
	flags.StringArrayVar(&logsOptions.excludes, "exclude", nil, "skip events with key[=value]; repeatable")
	flags.Int64Var(&logsOptions.id, "id", 0, "print one event by id")
	flags.StringVar(&logsOptions.from, "from", "", "only events since RFC 3339 time or age (30m, 6h, 2d)")
	flags.StringVar(&logsOptions.to, "to", "", "only events until RFC 3339 time or age")
	flags.IntVar(&logsOptions.limit, "limit", logsDefaultLimit, "newest events to print")
	flags.BoolVarP(&logsOptions.follow, "follow", "f", false, "keep waiting for new events")
	flags.BoolVar(&logsOptions.asJSON, "json", false, "print one JSON object per line")
	return command
}

func runLogs(command *cobra.Command, _ []string) error {
	query, err := buildEventsQuery(command)
	if err != nil {
		return err
	}
	if logsOptions.follow && command.Flags().Changed("id") {
		return errors.New("--follow and --id cannot be combined")
	}
	applicationConfig, _, err := config.Load()
	if err != nil {
		return err
	}
	database, err := db.OpenEventsReadOnly(applicationConfig.EventsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no events database at %s: the server has not run yet", applicationConfig.EventsPath)
		}
		return err
	}
	defer database.Release()
	service, err := events.New(database, applicationConfig.EventsRetentionDays)
	if err != nil {
		return fmt.Errorf("open events: %w", err)
	}
	fmt.Fprintf(os.Stderr, "reading %s\n", applicationConfig.EventsPath)

	ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt)
	defer stop()
	entries, err := service.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("query events: %w", err)
	}
	lastID := printEntries(sortedAscending(entries))
	if !logsOptions.follow {
		return nil
	}
	followQuery := query
	followQuery.Limit = logsFollowLimit
	ticker := time.NewTicker(logsFollowPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		entries, err := service.Query(ctx, followQuery)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("query events: %w", err)
		}
		if newest := printEntries(newerThan(entries, lastID)); newest > lastID {
			lastID = newest
		}
	}
}

func buildEventsQuery(command *cobra.Command) (events.Query, error) {
	query := events.Query{Limit: logsOptions.limit}
	if logsOptions.limit < 1 {
		return query, errors.New("--limit must be at least 1")
	}
	flags := command.Flags()
	if flags.Changed("severity") {
		severity, err := cli.ParseSeverity(logsOptions.severity)
		if err != nil {
			return query, err
		}
		query.Severity = &severity
	}
	if flags.Changed("id") {
		identifier := logsOptions.id
		query.ID = &identifier
	}
	now := time.Now()
	if flags.Changed("from") {
		start, err := cli.ParseTimeBound(logsOptions.from, now)
		if err != nil {
			return query, err
		}
		query.Start = &start
	}
	if flags.Changed("to") {
		end, err := cli.ParseTimeBound(logsOptions.to, now)
		if err != nil {
			return query, err
		}
		query.End = &end
	}
	if logsOptions.subsystem != "" {
		subsystem := logsOptions.subsystem
		query.Filters = append(query.Filters, events.KeyFilter{Key: logsSubsystemKey, Value: &subsystem})
	}
	for _, raw := range logsOptions.filters {
		key, value, err := cli.ParseKeyFilter(raw)
		if err != nil {
			return query, err
		}
		query.Filters = append(query.Filters, events.KeyFilter{Key: key, Value: value})
	}
	for _, raw := range logsOptions.excludes {
		key, value, err := cli.ParseKeyFilter(raw)
		if err != nil {
			return query, err
		}
		query.Excludes = append(query.Excludes, events.KeyFilter{Key: key, Value: value})
	}
	return query, nil
}

// printEntries prints in the given order and returns the highest id.
func printEntries(entries []events.Entry) int64 {
	var highest int64
	encoder := json.NewEncoder(os.Stdout)
	for _, entry := range entries {
		if logsOptions.asJSON {
			_ = encoder.Encode(entry)
		} else {
			fmt.Println(formatEntry(entry))
		}
		if entry.ID > highest {
			highest = entry.ID
		}
	}
	return highest
}

// sortedAscending reorders the newest-first query result oldest-first.
func sortedAscending(entries []events.Entry) []events.Entry {
	ordered := make([]events.Entry, len(entries))
	copy(ordered, entries)
	sort.Slice(ordered, func(first, second int) bool { return ordered[first].ID < ordered[second].ID })
	return ordered
}

// newerThan keeps entries above lastID, oldest-first.
func newerThan(entries []events.Entry, lastID int64) []events.Entry {
	var selected []events.Entry
	for _, entry := range entries {
		if entry.ID > lastID {
			selected = append(selected, entry)
		}
	}
	return sortedAscending(selected)
}

func formatEntry(entry events.Entry) string {
	var builder strings.Builder
	builder.WriteString("[")
	builder.WriteString(severityName(entry.Severity))
	builder.WriteString("][")
	builder.WriteString(entry.Timestamp.Format(logsTimestampForm))
	builder.WriteString("]")
	for _, key := range entry.Keys {
		builder.WriteString("[" + key.Key + ": ")
		if key.Value != nil {
			builder.WriteString(*key.Value)
		}
		builder.WriteString("]")
	}
	if entry.Message != "" {
		builder.WriteString(" " + entry.Message)
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
