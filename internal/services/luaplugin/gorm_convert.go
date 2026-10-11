package luaplugin

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
)

// pluginDeclarationsFromRecord builds the plugin-owned JSON declarations from
// the wire record. Type keys, handlers, icons, model specifications, health
// cooldowns, proxy source keys, schemas and jobs stay JSON because the shape
// is plugin-owned. Allow hosts, history, source, origin and timestamps live
// in columns or child tables, never in declarations.
func pluginDeclarationsFromRecord(pluginRecord *PluginRecord) map[string]any {
	declarations := map[string]any{}
	if pluginRecord == nil {
		return declarations
	}
	declarations["type_keys"] = copyStringSlice(pluginRecord.TypeKeys)
	declarations["handlers"] = pluginRecord.Handlers
	declarations["icons"] = pluginRecord.Icons
	declarations["model_specs"] = pluginRecord.ModelSpecifications
	declarations["health_cooldown"] = pluginRecord.HealthCooldown
	declarations["proxy_source_keys"] = copyStringSlice(pluginRecord.ProxySourceKeys)
	declarations["schemas"] = pluginRecord.Schemas
	declarations["jobs"] = pluginRecord.Jobs
	return declarations
}

// pluginRowFromRecord maps the wire record onto the plugins table row.
// OriginRepositoryID stays nil for manual installs or empty repository
// identifiers so the foreign key never references a missing repository row.
// Manual installs fail closed through the not-null origin columns, not
// through a dangling reference. Timestamps normalize to UTC.
func pluginRowFromRecord(pluginRecord *PluginRecord, installedAt time.Time, updatedAt time.Time) record.Plugin {
	var repositoryID *string
	if pluginRecord != nil && !pluginRecord.Origin.Manual && pluginRecord.Origin.RepoID != "" {
		copied := pluginRecord.Origin.RepoID
		repositoryID = &copied
	}
	row := record.Plugin{
		ID:               pluginRecord.ID,
		DisplayName:      pluginRecord.DisplayName,
		Author:           pluginRecord.Author,
		Version:          pluginRecord.Version,
		PluginAPIVersion: pluginRecord.PluginAPI,
		Description:      pluginRecord.Description,
		License:          pluginRecord.License,
		Unsafe:           pluginRecord.Unsafe,
		OriginPath:       pluginRecord.Origin.Path,
		OriginManual:     pluginRecord.Origin.Manual,
		Declarations:     pluginDeclarationsFromRecord(pluginRecord),
		Source:           append([]byte(nil), pluginRecord.Source...),
		InstalledAt:      installedAt.UTC(),
		UpdatedAt:        updatedAt.UTC(),
	}
	row.OriginRepositoryID = repositoryID
	return row
}

// pluginRecordFromRows rebuilds the wire record from the base row plus its
// allow host and version child rows. Declarations decode through a JSON
// round trip so numbers, nested maps and pointer slices return in
// plugin-visible shapes. Hosts sort for determinism; history orders by
// creation time with identifier tiebreak so rollback pops the most recent
// entry first. Timestamps return in UTC.
func pluginRecordFromRows(pluginRow record.Plugin, hostRows []record.PluginAllowHost, versionRows []record.PluginVersion) (*PluginRecord, error) {
	pluginRecord := &PluginRecord{
		ID:          pluginRow.ID,
		DisplayName: pluginRow.DisplayName,
		Author:      pluginRow.Author,
		Version:     pluginRow.Version,
		PluginAPI:   pluginRow.PluginAPIVersion,
		Description: pluginRow.Description,
		License:     pluginRow.License,
		Unsafe:      pluginRow.Unsafe,
		Source:      append([]byte(nil), pluginRow.Source...),
		InstalledAt: pluginRow.InstalledAt.UTC(),
		UpdatedAt:   pluginRow.UpdatedAt.UTC(),
	}
	if pluginRow.OriginRepositoryID != nil {
		pluginRecord.Origin.RepoID = *pluginRow.OriginRepositoryID
	}
	pluginRecord.Origin.Path = pluginRow.OriginPath
	pluginRecord.Origin.Manual = pluginRow.OriginManual
	hosts := make([]string, 0, len(hostRows))
	for _, hostRow := range hostRows {
		hosts = append(hosts, hostRow.Host)
	}
	sort.Strings(hosts)
	pluginRecord.AllowHosts = hosts
	declarations := pluginRow.Declarations
	if err := decodeDeclarationsField(declarations, "type_keys", &pluginRecord.TypeKeys); err != nil {
		return nil, fmt.Errorf("decode type keys: %w", err)
	}
	if err := decodeDeclarationsField(declarations, "handlers", &pluginRecord.Handlers); err != nil {
		return nil, fmt.Errorf("decode handlers: %w", err)
	}
	if err := decodeDeclarationsField(declarations, "icons", &pluginRecord.Icons); err != nil {
		return nil, fmt.Errorf("decode icons: %w", err)
	}
	if err := decodeDeclarationsField(declarations, "model_specs", &pluginRecord.ModelSpecifications); err != nil {
		return nil, fmt.Errorf("decode model specifications: %w", err)
	}
	if err := decodeDeclarationsField(declarations, "health_cooldown", &pluginRecord.HealthCooldown); err != nil {
		return nil, fmt.Errorf("decode health cooldown: %w", err)
	}
	if err := decodeDeclarationsField(declarations, "proxy_source_keys", &pluginRecord.ProxySourceKeys); err != nil {
		return nil, fmt.Errorf("decode proxy source keys: %w", err)
	}
	if err := decodeDeclarationsField(declarations, "schemas", &pluginRecord.Schemas); err != nil {
		return nil, fmt.Errorf("decode schemas: %w", err)
	}
	if err := decodeDeclarationsField(declarations, "jobs", &pluginRecord.Jobs); err != nil {
		return nil, fmt.Errorf("decode jobs: %w", err)
	}
	orderedVersions := append([]record.PluginVersion(nil), versionRows...)
	sort.Slice(orderedVersions, func(first int, second int) bool {
		if orderedVersions[first].CreatedAt.Equal(orderedVersions[second].CreatedAt) {
			return orderedVersions[first].ID < orderedVersions[second].ID
		}
		return orderedVersions[first].CreatedAt.Before(orderedVersions[second].CreatedAt)
	})
	history := make([]PluginVersionSnapshot, 0, len(orderedVersions))
	for _, versionRow := range orderedVersions {
		snapshot, err := snapshotFromVersionRow(versionRow)
		if err != nil {
			return nil, err
		}
		history = append(history, snapshot)
	}
	pluginRecord.History = history
	return pluginRecord, nil
}

// versionDeclarationsFromSnapshot builds the plugin-owned JSON declarations
// for one history entry. The keys match pluginDeclarationsFromRecord so
// rollback restores the identical shapes.
func versionDeclarationsFromSnapshot(snapshot PluginVersionSnapshot) map[string]any {
	return map[string]any{
		"type_keys":         copyStringSlice(snapshot.TypeKeys),
		"handlers":          snapshot.Handlers,
		"icons":             snapshot.Icons,
		"model_specs":       snapshot.ModelSpecifications,
		"health_cooldown":   snapshot.HealthCooldown,
		"proxy_source_keys": copyStringSlice(snapshot.ProxySourceKeys),
		"schemas":           snapshot.Schemas,
		"jobs":              snapshot.Jobs,
	}
}

// versionRowFromSnapshot maps one history snapshot onto a plugin_versions
// row. The identifier is generated outside so writes stay deterministic in
// tests. Creation time normalizes to UTC.
func versionRowFromSnapshot(pluginID string, snapshot PluginVersionSnapshot, identifier string, createdAt time.Time) record.PluginVersion {
	return record.PluginVersion{
		ID:           identifier,
		PluginID:     pluginID,
		Version:      snapshot.Version,
		Source:       append([]byte(nil), snapshot.Source...),
		Declarations: versionDeclarationsFromSnapshot(snapshot),
		CreatedAt:    createdAt.UTC(),
	}
}

// snapshotFromVersionRow rebuilds one history snapshot from a
// plugin_versions row. Declarations decode through the same JSON round trip
// as the base row so rollback restores plugin-visible shapes.
func snapshotFromVersionRow(versionRow record.PluginVersion) (PluginVersionSnapshot, error) {
	snapshot := PluginVersionSnapshot{
		Version: versionRow.Version,
		Source:  append([]byte(nil), versionRow.Source...),
	}
	declarations := versionRow.Declarations
	if err := decodeDeclarationsField(declarations, "type_keys", &snapshot.TypeKeys); err != nil {
		return PluginVersionSnapshot{}, fmt.Errorf("decode version type keys: %w", err)
	}
	if err := decodeDeclarationsField(declarations, "handlers", &snapshot.Handlers); err != nil {
		return PluginVersionSnapshot{}, fmt.Errorf("decode version handlers: %w", err)
	}
	if err := decodeDeclarationsField(declarations, "icons", &snapshot.Icons); err != nil {
		return PluginVersionSnapshot{}, fmt.Errorf("decode version icons: %w", err)
	}
	if err := decodeDeclarationsField(declarations, "model_specs", &snapshot.ModelSpecifications); err != nil {
		return PluginVersionSnapshot{}, fmt.Errorf("decode version model specifications: %w", err)
	}
	if err := decodeDeclarationsField(declarations, "health_cooldown", &snapshot.HealthCooldown); err != nil {
		return PluginVersionSnapshot{}, fmt.Errorf("decode version health cooldown: %w", err)
	}
	if err := decodeDeclarationsField(declarations, "proxy_source_keys", &snapshot.ProxySourceKeys); err != nil {
		return PluginVersionSnapshot{}, fmt.Errorf("decode version proxy source keys: %w", err)
	}
	if err := decodeDeclarationsField(declarations, "schemas", &snapshot.Schemas); err != nil {
		return PluginVersionSnapshot{}, fmt.Errorf("decode version schemas: %w", err)
	}
	if err := decodeDeclarationsField(declarations, "jobs", &snapshot.Jobs); err != nil {
		return PluginVersionSnapshot{}, fmt.Errorf("decode version jobs: %w", err)
	}
	return snapshot, nil
}

// snapshotFromRecord builds one history snapshot from the current wire
// record before an update or rollback overwrites it.
func snapshotFromRecord(pluginRecord *PluginRecord) PluginVersionSnapshot {
	return PluginVersionSnapshot{
		Version:             pluginRecord.Version,
		Source:              append([]byte(nil), pluginRecord.Source...),
		TypeKeys:            append([]string(nil), pluginRecord.TypeKeys...),
		Handlers:            pluginRecord.Handlers,
		Icons:               pluginRecord.Icons,
		ModelSpecifications: pluginRecord.ModelSpecifications,
		HealthCooldown:      pluginRecord.HealthCooldown,
		ProxySourceKeys:     append([]string(nil), pluginRecord.ProxySourceKeys...),
		Schemas:             pluginRecord.Schemas,
		Jobs:                pluginRecord.Jobs,
	}
}

// allowHostRowsFromRecord maps the allow host list onto one
// plugin_allow_hosts row per host.
func allowHostRowsFromRecord(pluginRecord *PluginRecord) []record.PluginAllowHost {
	rows := make([]record.PluginAllowHost, 0, len(pluginRecord.AllowHosts))
	for _, host := range pluginRecord.AllowHosts {
		rows = append(rows, record.PluginAllowHost{PluginID: pluginRecord.ID, Host: host})
	}
	return rows
}

// decodeDeclarationsField decodes one declarations entry through a JSON
// round trip into the target pointer. Missing or null entries leave the
// target untouched so absent shapes stay absent instead of empty.
func decodeDeclarationsField(declarations map[string]any, field string, target any) error {
	if declarations == nil {
		return nil
	}
	rawValue, exists := declarations[field]
	if !exists || rawValue == nil {
		return nil
	}
	encoded, err := json.Marshal(rawValue)
	if err != nil {
		return fmt.Errorf("marshal declarations field %q: %w", field, err)
	}
	// JSON null decodes as absent: leave the target untouched.
	if string(encoded) == "null" {
		return nil
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		return fmt.Errorf("unmarshal declarations field %q: %w", field, err)
	}
	return nil
}

// copyStringSlice copies a string slice while preserving nil versus empty.
// A nil input stays nil; a non-nil input returns a fresh non-nil slice.
func copyStringSlice(values []string) []string {
	if values == nil {
		return nil
	}
	copied := make([]string, len(values))
	copy(copied, values)
	return copied
}

// storageValueToBytes encodes one plugin storage value to JSON bytes for
// the value column. A nil value encodes as null so the not-null column
// always holds a payload.
func storageValueToBytes(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal storage value: %w", err)
	}
	if len(encoded) == 0 {
		return []byte("null"), nil
	}
	return encoded, nil
}

// storageValueFromBytes decodes one plugin storage value from JSON bytes.
// Empty payloads read as missing, matching the bbolt envelope behavior for
// absent rows.
func storageValueFromBytes(raw []byte) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("decode storage value: %w", err)
	}
	return value, nil
}
