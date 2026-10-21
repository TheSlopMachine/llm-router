package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Key describes one deployment setting stored in the dotenv file. The
// table below is the single owner of names, defaults and validation: the
// default template, `llm-router config` and the help text all derive from it.
type Key struct {
	Name        string
	Default     string
	Description string
	normalize   func(raw string) (string, error)
}

// Normalize validates raw and returns the canonical text stored in the
// dotenv file. A blank value fails: unset the key to restore the default.
func (key Key) Normalize(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("value must not be empty")
	}
	return key.normalize(raw)
}

var keys = []Key{
	{Name: "HOST", Default: DefaultHost, Description: "listen address", normalize: normalizeHost},
	{Name: "PORT", Default: DefaultPort, Description: "listen port for dashboard, /api and /v1", normalize: normalizePort},
	{Name: "DB_PATH", Default: DefaultDatabasePath, Description: "main database file", normalize: normalizePath},
	{Name: "CACHE_PATH", Default: DefaultCachePath, Description: "rebuildable cache database file", normalize: normalizePath},
	{Name: "EVENTS_PATH", Default: DefaultEventsPath, Description: "events (logs) database file", normalize: normalizePath},
	{Name: "EVENTS_RETENTION_DAYS", Default: strconv.Itoa(DefaultEventsRetentionDays), Description: "days to keep events", normalize: normalizeRetention},
	{Name: "LOG_LEVEL", Default: DefaultLogLevel, Description: "debug, info, warn or error", normalize: normalizeLogLevel},
	{Name: "LUA_POOL_BUCKET_SIZE", Default: strconv.Itoa(DefaultLuaPoolBucketSize), Description: "idle Lua states kept per plugin, 0 disables the pool", normalize: normalizePoolSize},
}

// pathKeyNames lists the keys that address database files. They must
// resolve to distinct files.
var pathKeyNames = []string{"DB_PATH", "CACHE_PATH", "EVENTS_PATH"}

// Keys returns the configuration keys in display order.
func Keys() []Key {
	result := make([]Key, len(keys))
	copy(result, keys)
	return result
}

// LookupKey finds a key by name, ignoring case.
func LookupKey(name string) (Key, bool) {
	trimmed := strings.TrimSpace(name)
	for _, key := range keys {
		if strings.EqualFold(key.Name, trimmed) {
			return key, true
		}
	}
	return Key{}, false
}

// CheckDistinctPaths fails when two of the database path keys in values
// resolve to the same file. Missing keys are skipped.
func CheckDistinctPaths(values map[string]string) error {
	for first := 0; first < len(pathKeyNames); first++ {
		firstValue, firstOK := expandedPathValue(values, pathKeyNames[first])
		if !firstOK {
			continue
		}
		for second := first + 1; second < len(pathKeyNames); second++ {
			secondValue, secondOK := expandedPathValue(values, pathKeyNames[second])
			if secondOK && samePath(firstValue, secondValue) {
				return fmt.Errorf("%s and %s must differ", pathKeyNames[first], pathKeyNames[second])
			}
		}
	}
	return nil
}

func expandedPathValue(values map[string]string, name string) (string, bool) {
	raw, found := values[name]
	if !found {
		return "", false
	}
	expanded, err := ExpandPath(raw)
	if err != nil || expanded == "" {
		return "", false
	}
	return expanded, true
}

// DefaultTemplate renders the default .env content. .env.example mirrors it.
func DefaultTemplate() string {
	var builder strings.Builder
	builder.WriteString("# llm-router configuration file\n")
	for _, key := range keys {
		builder.WriteString(key.Name + "=" + key.Default + "\n")
	}
	return builder.String()
}

func normalizeHost(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if strings.ContainsAny(value, " \t/") {
		return "", errors.New("must be a host name or an address")
	}
	return value, nil
}

func normalizePort(raw string) (string, error) {
	port, err := parsePortValue(raw)
	if err != nil {
		return "", err
	}
	return port.String(), nil
}

func normalizePath(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	expanded, err := ExpandPath(value)
	if err != nil {
		return "", err
	}
	if expanded == "" {
		return "", errors.New("path must not be empty")
	}
	if strings.ContainsAny(value, "'\n\r") {
		return "", errors.New("path must not contain quotes or line breaks")
	}
	return value, nil
}

func normalizeRetention(raw string) (string, error) {
	days, err := parseEventsRetentionValue(raw)
	if err != nil {
		return "", err
	}
	return strconv.Itoa(days), nil
}

func normalizeLogLevel(raw string) (string, error) {
	level, err := parseLogLevelValue(raw)
	if err != nil {
		return "", err
	}
	return string(level), nil
}

func normalizePoolSize(raw string) (string, error) {
	size, err := parseLuaPoolBucketSizeValue(raw)
	if err != nil {
		return "", err
	}
	return strconv.Itoa(size), nil
}
