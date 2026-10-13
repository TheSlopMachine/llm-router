package luaplugin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/events"
	lua "github.com/yuin/gopher-lua"
	"gorm.io/gorm"
)

// maxPluginSource caps a single plugin file at 1 MiB.
const maxPluginSource = 1 << 20

// PluginOrigin describes where a plugin was installed from.
type PluginOrigin struct {
	RepoID string `json:"repo_id"`
	Path   string `json:"path"`
	Manual bool   `json:"manual"`
}

// JobSpec is the validated schedule of one colocated plugin job.
type JobSpec struct {
	IntervalSeconds int64 `json:"interval_seconds"`
	RunOnStartup    bool  `json:"run_on_startup"`
	TimeoutMs       int   `json:"timeout_ms"`
}

// PluginVersionSnapshot is one rollback entry.
type PluginVersionSnapshot struct {
	Version             string                                 `json:"version"`
	Source              []byte                                 `json:"source"`
	TypeKeys            []string                               `json:"type_keys"`
	Handlers            map[string][]string                    `json:"handlers"`
	Icons               map[string]string                      `json:"icons"`
	ModelSpecifications map[string]map[string]models.ModelInfo `json:"model_specs,omitempty"`
	HealthCooldown      map[string]int64                       `json:"health_cooldown,omitempty"`
	ProxySourceKeys     []string                               `json:"proxy_source_keys,omitempty"`
	Schemas             map[string]map[string][]*models.UINode `json:"schemas,omitempty"`
	Locales             map[string]PluginLocales               `json:"locales,omitempty"`
	Jobs                map[string]map[string]JobSpec          `json:"jobs,omitempty"`
}

// PluginRecord is the stored plugin row in the plugins table.
type PluginRecord struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Author      string   `json:"author"`
	Version     string   `json:"version"`
	PluginAPI   string   `json:"plugin_api_version"`
	Description string   `json:"description"`
	License     string   `json:"license"`
	AllowHosts  []string `json:"allow_hosts"`
	Unsafe      bool     `json:"unsafe"`
	Disabled    bool     `json:"disabled"`
	// ProxySourceKeys lists registered proxy-list feeds in this plugin.
	ProxySourceKeys []string `json:"proxy_source_keys,omitempty"`
	// ModelSpecifications holds per-type pinned model rows from the registration
	// model_specs table, merged over discovered rows in GetModelInfos.
	ModelSpecifications map[string]map[string]models.ModelInfo `json:"model_specs,omitempty"`
	// HealthCooldown holds per-type healthcheck_cooldown values in seconds
	// from the registration table. Absent means DefaultHealthCooldown.
	HealthCooldown map[string]int64 `json:"health_cooldown,omitempty"`
	// Schemas holds per-type static UI tables: credential_schema,
	// config_schema, settings_schema, proxy_schema. A missing table means
	// the surface stays hidden.
	Schemas map[string]map[string][]*models.UINode `json:"schemas,omitempty"`
	// Locales holds per-type widget translations: locale code -> widget
	// key -> localized text. Absent means widgets serve stored text.
	Locales map[string]PluginLocales `json:"locales,omitempty"`
	// Jobs holds per-type colocated job schedules. Function bodies live in
	// plugin source and load fresh on every run.
	Jobs        map[string]map[string]JobSpec `json:"jobs,omitempty"`
	TypeKeys    []string                      `json:"type_keys"`
	Handlers    map[string][]string           `json:"handlers"`
	Icons       map[string]string             `json:"icons"`
	Source      []byte                        `json:"source"`
	History     []PluginVersionSnapshot       `json:"history"`
	Origin      PluginOrigin                  `json:"origin"`
	InstalledAt time.Time                     `json:"installed_at"`
	UpdatedAt   time.Time                     `json:"updated_at"`
}

// LogEntry is one plugin log line read back from the events database.
type LogEntry struct {
	At       time.Time         `json:"at"`
	Severity int               `json:"severity"`
	Message  string            `json:"message"`
	Keys     map[string]string `json:"keys"`
}

// CrashEntry is one recorded PluginInternalError.
type CrashEntry struct {
	At      time.Time `json:"at"`
	TypeKey string    `json:"type_key"`
	Cause   string    `json:"cause"`
}

// Service owns the plugin lifecycle, the type key registry and handler calls.
// Plugin rows live in SQLite; the in-memory registry rebuilds from it after
// every lifecycle mutation.
type Service struct {
	// database is the base SQLite handle. Only the base handle is stored,
	// never a chained query.
	database *gorm.DB
	// cache is the cache SQLite handle. TTL plugin storage rows live here;
	// durable rows stay on the base handle.
	cache *gorm.DB

	mu       sync.RWMutex
	registry map[string]*PluginRecord
	// disabledTypes tracks type keys owned by installed but disabled
	// plugins. Lookup maps them to ErrPluginDisabled; the registry
	// serves enabled types only.
	disabledTypes map[string]bool

	eventsService *events.Service

	logMu   sync.Mutex
	crashes map[string][]CrashEntry

	// wiringMu guards every mutable wiring field below: logger,
	// minimumLevel, onChanged and all credential, proxy and health
	// callbacks. Handler paths snapshot them under RLock; Set methods
	// replace them under Lock so detached lifecycle hooks never race
	// with late wiring.
	wiringMu     sync.RWMutex
	onChanged    func(typeKey string)
	logger       *slog.Logger
	minimumLevel slog.Level

	// healthTrigger receives failed attempt identities for detached
	// health-check dispatch (nil = disabled).
	healthTrigger HealthTrigger

	// markDead excludes one proxy URL until an escalating ban expires
	// (nil = disabled). Called only from the plugin HTTP client on
	// structural TLS faults; plugins never call it directly.
	markDead func(url, reason string) bool

	// proxyQuery returns read-only proxy endpoints for plugin selection.
	proxyQuery func(pool, country string, limit int) ([]models.ProxyView, error)

	// proxyRequire serves proxies.require: it waits for matching proxies
	// within the request context (nil = disabled).
	proxyRequire func(ctx context.Context, request models.ProxyRequire) (models.ProxyRequireResult, error)

	// credential access for plugins. list/get serve request and job
	// contexts; update serves job contexts only; disable/enable serve
	// every context but require the automation switch (see automationOn).
	credentialList    func(ctx context.Context, providerID string) ([]*models.Credential, error)
	credentialGet     func(ctx context.Context, id string) (*models.Credential, error)
	credentialUpdate  func(ctx context.Context, id string, data map[string]any) error
	credentialDisable func(ctx context.Context, id string, reason string) error
	credentialEnable  func(ctx context.Context, id string) error
	credentialPark    func(ctx context.Context, id string, ttl time.Duration, reason string) error
	credentialUnpark  func(ctx context.Context, id string) error
	credentialParked  func(ctx context.Context, id string) (*models.ParkEntry, error)
	// automationOn reports the provider disable_failed_credentials switch
	// gating shared disable/enable writes; nil reads off.
	automationOn func(providerID string) bool
}

// SetHealthTrigger wires detached health-check dispatch for failed
// attempts that carry a credential identity.
func (s *Service) SetHealthTrigger(trigger HealthTrigger) {
	s.wiringMu.Lock()
	defer s.wiringMu.Unlock()
	s.healthTrigger = trigger
}

// SetMarkDead wires proxy exclusion for structural TLS faults observed by
// the plugin HTTP client.
func (s *Service) SetMarkDead(mark func(url, reason string) bool) {
	s.wiringMu.Lock()
	defer s.wiringMu.Unlock()
	s.markDead = mark
}

// SetProxyQuery wires the read-only proxy pool query for plugins.
func (s *Service) SetProxyQuery(query func(pool, country string, limit int) ([]models.ProxyView, error)) {
	s.wiringMu.Lock()
	defer s.wiringMu.Unlock()
	s.proxyQuery = query
}

// SetProxyRequire wires the demand-driven proxy acquisition for plugins.
func (s *Service) SetProxyRequire(require func(ctx context.Context, request models.ProxyRequire) (models.ProxyRequireResult, error)) {
	s.wiringMu.Lock()
	defer s.wiringMu.Unlock()
	s.proxyRequire = require
}

// SetCredentialAccess wires credential list/get (request and job contexts)
// and update (job contexts only) for plugins.
func (s *Service) SetCredentialAccess(
	list func(ctx context.Context, providerID string) ([]*models.Credential, error),
	get func(ctx context.Context, id string) (*models.Credential, error),
	update func(ctx context.Context, id string, data map[string]any) error,
) {
	s.wiringMu.Lock()
	defer s.wiringMu.Unlock()
	s.credentialList = list
	s.credentialGet = get
	s.credentialUpdate = update
}

// SetCredentialLifecycle wires plugin disable/enable writes plus the
// automation-switch gate for the shared disabled state.
func (s *Service) SetCredentialLifecycle(
	disable func(ctx context.Context, id string, reason string) error,
	enable func(ctx context.Context, id string) error,
	automationOn func(providerID string) bool,
) {
	s.wiringMu.Lock()
	defer s.wiringMu.Unlock()
	s.credentialDisable = disable
	s.credentialEnable = enable
	s.automationOn = automationOn
}

// SetCredentialParks wires the unified cooldown park store for plugins.
func (s *Service) SetCredentialParks(
	park func(ctx context.Context, id string, ttl time.Duration, reason string) error,
	unpark func(ctx context.Context, id string) error,
	parked func(ctx context.Context, id string) (*models.ParkEntry, error),
) {
	s.wiringMu.Lock()
	defer s.wiringMu.Unlock()
	s.credentialPark = park
	s.credentialUnpark = unpark
	s.credentialParked = parked
}

// SetLogger wires structured logging.
func (s *Service) SetLogger(logger *slog.Logger) {
	s.wiringMu.Lock()
	defer s.wiringMu.Unlock()
	s.logger = logger
}

// SetMinimumLevel bounds plugin log persistence: severities mapping
// below the level never reach the events database.
func (s *Service) SetMinimumLevel(level slog.Level) {
	s.wiringMu.Lock()
	defer s.wiringMu.Unlock()
	s.minimumLevel = level
}

// SetOnChanged registers a callback fired with every affected type key
// after install/update/rollback/delete/enable/disable.
func (s *Service) SetOnChanged(callback func(typeKey string)) {
	s.wiringMu.Lock()
	defer s.wiringMu.Unlock()
	s.onChanged = callback
}

// getLogger returns the configured logger under the wiring lock.
func (s *Service) getLogger() *slog.Logger {
	s.wiringMu.RLock()
	defer s.wiringMu.RUnlock()
	return s.logger
}

// getMinimumLevel returns the log persistence floor under the wiring lock.
func (s *Service) getMinimumLevel() slog.Level {
	s.wiringMu.RLock()
	defer s.wiringMu.RUnlock()
	return s.minimumLevel
}

func (s *Service) notify(keys ...string) {
	s.wiringMu.RLock()
	callback := s.onChanged
	s.wiringMu.RUnlock()
	if callback == nil {
		return
	}
	for _, k := range keys {
		callback(k)
	}
}

// rebuild reloads the in-memory type key registry from the SQLite plugin
// rows. Disabled plugins stay inventoried but leave the registry; their
// type keys land in disabledTypes so Lookup maps them to ErrPluginDisabled.
// Conflicting type keys across enabled plugins fail loudly: resolve by
// uninstalling one.
func (s *Service) rebuild(ctx context.Context) error {
	records, err := s.ListSQLite(ctx)
	if err != nil {
		return err
	}
	reg := map[string]*PluginRecord{}
	disabled := map[string]bool{}
	for _, record := range records {
		if record.Disabled {
			for _, key := range record.TypeKeys {
				disabled[key] = true
			}
			continue
		}
		if err := CheckPluginAPI(&Manifest{PluginAPI: record.PluginAPI}); err != nil {
			if logger := s.getLogger(); logger != nil {
				reason := "invalid"
				var compatError *APICompatError
				if errors.As(err, &compatError) {
					reason = compatError.Reason
				}
				logger.Warn("Plugin API incompatible, skipping plugin", "plugin_id", record.ID, "reason", reason, "error", err.Error())
			}
			continue
		}
		for _, key := range record.TypeKeys {
			if prev, exists := reg[key]; exists {
				return fmt.Errorf("duplicate type key %q across plugins %q and %q: resolve by uninstalling one", key, prev.ID, record.ID)
			}
			cp := *record
			reg[key] = &cp
		}
	}
	s.mu.Lock()
	s.registry = reg
	s.disabledTypes = disabled
	s.mu.Unlock()
	return nil
}

// Lookup returns the plugin record serving typeKey. Types owned by a
// disabled plugin fail with ErrPluginDisabled; unknown types fail without
// the sentinel so callers keep the missing-backend path.
func (s *Service) Lookup(typeKey string) (*PluginRecord, error) {
	s.mu.RLock()
	record, ok := s.registry[typeKey]
	disabled := s.disabledTypes[typeKey]
	s.mu.RUnlock()
	if ok {
		return record, nil
	}
	if disabled {
		return nil, fmt.Errorf("plugin for type key %q is disabled: %w", typeKey, ErrPluginDisabled)
	}
	return nil, fmt.Errorf("no plugin registered for type key %q", typeKey)
}

// TypeDisabled reports whether typeKey belongs to an installed but disabled
// plugin. Enabled and unknown types report false.
func (s *Service) TypeDisabled(typeKey string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.disabledTypes[typeKey]
}

// capabilitiesForRecord resolves sandbox table gating for one type key from
// a stored record snapshot. The lifecycle hooks execute snapshots (the
// registry may already serve a newer source), so they pass the record they
// run instead of reading the registry.
func capabilitiesForRecord(record *PluginRecord, typeKey string) capabilities {
	caps := capabilities{}
	if record == nil {
		return caps
	}
	if nodes, ok := record.Schemas[typeKey]["credential_schema"]; ok && nodes != nil {
		caps.credentials = true
	} else {
		for _, h := range record.Handlers[typeKey] {
			if h == string(HandlerAuthInitiate) {
				caps.credentials = true
				break
			}
		}
	}
	_, caps.proxies = record.Schemas[typeKey]["proxy_schema"]
	return caps
}

// capabilitiesFor resolves sandbox table gating for one type key from the
// stored record.
func (s *Service) capabilitiesFor(typeKey string) capabilities {
	s.mu.RLock()
	record, ok := s.registry[typeKey]
	s.mu.RUnlock()
	if !ok {
		return capabilities{}
	}
	return capabilitiesForRecord(record, typeKey)
}

// CredentialsEnabled reports whether a type key serves credentials:
// a credential_schema table or an auth_initiate handler is present.
func (s *Service) CredentialsEnabled(typeKey string) bool {
	if s.HasHandler(typeKey, string(HandlerAuthInitiate)) {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.registry[typeKey]
	if !ok {
		return false
	}
	nodes, ok := record.Schemas[typeKey]["credential_schema"]
	return ok && nodes != nil
}

// ProxiesEnabled reports whether a type key serves proxy settings:
// a proxy_schema table is present (empty tables enable defaults).
func (s *Service) ProxiesEnabled(typeKey string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.registry[typeKey]
	if !ok {
		return false
	}
	_, ok = record.Schemas[typeKey]["proxy_schema"]
	return ok
}

// Schemas returns the stored static UI tables for a type key, or nil when
// the plugin declares none.
func (s *Service) Schemas(typeKey string) map[string][]*models.UINode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.registry[typeKey]
	if !ok {
		return nil
	}
	return record.Schemas[typeKey]
}

// Jobs returns the stored job schedules for a type key, or nil.
func (s *Service) Jobs(typeKey string) map[string]JobSpec {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.registry[typeKey]
	if !ok {
		return nil
	}
	return record.Jobs[typeKey]
}

// Registered returns all enabled type keys, sorted.
func (s *Service) Registered() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.registry))
	for k := range s.registry {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Get returns a plugin record by composite ID.
// maxPluginIconBytes caps an icon value (data-URI icons live in SQLite).
const maxPluginIconBytes = 32 << 10

// validateIcon accepts empty (no icon), https:// URLs and data:image URIs.
func validateIcon(typeKey, value string) error {
	if value == "" {
		return nil
	}
	if len(value) > maxPluginIconBytes {
		return fmt.Errorf("plugin type %q: icon exceeds %d bytes", typeKey, maxPluginIconBytes)
	}
	if strings.HasPrefix(value, "https://") {
		return nil
	}
	if strings.HasPrefix(value, "data:image/") {
		return nil
	}
	return fmt.Errorf("plugin type %q: icon must be an https:// URL or data:image URI", typeKey)
}

// dryResult is the validated install-time surface of one plugin source.
type dryResult struct {
	keys           []string
	handlers       map[string][]string
	icons          map[string]string
	specifications map[string]map[string]models.ModelInfo
	cooldowns      map[string]int64
	sourceKeys     []string
	schemas        map[string]map[string][]*models.UINode
	locales        map[string]PluginLocales
	jobs           map[string]map[string]JobSpec
}

// schemaKinds are the static UI tables validated at install, never invoked.
var schemaKinds = []string{"credential_schema", "config_schema", "settings_schema", "proxy_schema"}

// dryRun executes the plugin top-level code in a fully configured sandbox
// and returns the validated registration surface. Handlers and job bodies
// are not invoked.
func (s *Service) dryRun(pluginID string, source []byte, manifest *Manifest) (*dryResult, error) {
	out := &dryResult{
		handlers:       map[string][]string{},
		icons:          map[string]string{},
		specifications: map[string]map[string]models.ModelInfo{},
		cooldowns:      map[string]int64{},
		schemas:        map[string]map[string][]*models.UINode{},
		locales:        map[string]PluginLocales{},
		jobs:           map[string]map[string]JobSpec{},
	}
	ctx := &executionContext{
		pluginID:      pluginID,
		allowHosts:    manifest.AllowHosts,
		unsafe:        manifest.Unsafe,
		logger:        s.getLogger(),
		logSink:       s.appendLog,
		storage:       &serviceStorage{service: s, ctx: context.Background()},
		registrations: map[string]*lua.LTable{},
		proxySources:  map[string]*lua.LTable{},
	}
	L := newSandboxState(ctx)
	defer L.Close()
	if err := L.DoString(string(source)); err != nil {
		// A parse failure pushes nothing: fall back to the Go error so
		// the message carries the parser diagnostics instead of "nil".
		cause := luaErrorString(L.Get(-1))
		if cause == "" || cause == "nil" {
			cause = err.Error()
		}
		return nil, &models.PluginInternalError{
			PluginID: pluginID, Cause: fmt.Sprintf("top-level: %s (source %d bytes)", cause, len(source)),
		}
	}
	if len(ctx.registrations) == 0 && len(ctx.proxySources) == 0 {
		return nil, fmt.Errorf("plugin declares no type keys: missing llm_router.register call")
	}
	for k := range ctx.proxySources {
		out.sourceKeys = append(out.sourceKeys, k)
	}
	sort.Strings(out.sourceKeys)
	for k, table := range ctx.registrations {
		out.keys = append(out.keys, k)
		var names []string
		for _, name := range AllHandlerNames() {
			if v := table.RawGetString(name); v != lua.LNil {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		out.handlers[k] = names
		if v := table.RawGetString("icon"); v != lua.LNil {
			icon, ok := v.(lua.LString)
			if !ok {
				return nil, fmt.Errorf("plugin type %q: icon must be a string", k)
			}
			if err := validateIcon(k, string(icon)); err != nil {
				return nil, err
			}
			if string(icon) != "" {
				out.icons[k] = string(icon)
			}
		}
		if v := table.RawGetString("model_specs"); v != lua.LNil {
			specificationTable, ok := v.(*lua.LTable)
			if !ok {
				return nil, fmt.Errorf("plugin type %q: model_specs must be a table", k)
			}
			parsed, err := parseModelSpecifications(specificationTable, k)
			if err != nil {
				return nil, err
			}
			out.specifications[k] = parsed
		}
		if v := table.RawGetString("healthcheck_cooldown"); v != lua.LNil {
			d, err := parseHealthCooldown(v, k)
			if err != nil {
				return nil, err
			}
			out.cooldowns[k] = int64(d / time.Second)
		}
		for _, kind := range schemaKinds {
			if v := table.RawGetString(kind); v != lua.LNil {
				schemaTbl, ok := v.(*lua.LTable)
				if !ok {
					return nil, fmt.Errorf("plugin type %q: %s must be a table", k, kind)
				}
				nodes, err := parseUINodes(schemaTbl)
				if err != nil {
					return nil, fmt.Errorf("plugin type %q: %s: %w", k, kind, err)
				}
				if out.schemas[k] == nil {
					out.schemas[k] = map[string][]*models.UINode{}
				}
				out.schemas[k][kind] = nodes
			}
		}
		if v := table.RawGetString("locales"); v != lua.LNil {
			parsed, err := ParseLocales(v, k)
			if err != nil {
				return nil, err
			}
			if parsed != nil {
				out.locales[k] = parsed
			}
		}
		if v := table.RawGetString("jobs"); v != lua.LNil {
			jobsTbl, ok := v.(*lua.LTable)
			if !ok {
				return nil, fmt.Errorf("plugin type %q: jobs must be a table", k)
			}
			parsed, err := parseJobSpecifications(jobsTbl, k)
			if err != nil {
				return nil, err
			}
			out.jobs[k] = parsed
		}
	}
	sort.Strings(out.keys)
	return out, nil
}

// HasHandler reports whether a type key declares a handler.
func (s *Service) HasHandler(typeKey, handler string) bool {
	s.mu.RLock()
	record, ok := s.registry[typeKey]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	for _, h := range record.Handlers[typeKey] {
		if h == handler {
			return true
		}
	}
	return false
}

// Icon returns the icon declared by a type key, or empty when none.
func (s *Service) Icon(typeKey string) string {
	s.mu.RLock()
	record, ok := s.registry[typeKey]
	s.mu.RUnlock()
	if !ok {
		return ""
	}
	return record.Icons[typeKey]
}

// BuildID derives the composite plugin ID from origin and manifest.
// Repo installs use the file basename: <repo>/<author>/<basename>.
// Manual uploads use: manual/<author>/<slug-of-plugin-name>.
func BuildID(origin PluginOrigin, manifest *Manifest) (string, error) {
	author, err := sanitizeIDPart(manifest.Author)
	if err != nil {
		return "", fmt.Errorf("author: %w", err)
	}
	if origin.Manual {
		name, err := sanitizeIDPart(manifest.Plugin)
		if err != nil {
			return "", fmt.Errorf("plugin name: %w", err)
		}
		return "manual/" + author + "/" + name, nil
	}
	if strings.TrimSpace(origin.RepoID) == "" {
		return "", fmt.Errorf("repo origin requires repo id")
	}
	if err := validateRepositoryID(origin.RepoID); err != nil {
		return "", err
	}
	base := path.Base(origin.Path)
	if !strings.HasSuffix(base, ".lua") {
		return "", fmt.Errorf("plugin path %q must end with .lua", origin.Path)
	}
	name := strings.TrimSuffix(base, ".lua")
	if name == "" || strings.Contains(name, "/") || name == "." || name == ".." {
		return "", fmt.Errorf("invalid plugin path %q", origin.Path)
	}
	dir := path.Dir(origin.Path)
	if dir != "llm-router-plugins" {
		return "", fmt.Errorf("plugin path %q must live directly in llm-router-plugins/", origin.Path)
	}
	return origin.RepoID + "/" + author + "/" + name, nil
}

// validateRepositoryID accepts slash-separated repository IDs such as
// "github/<owner>/<repo>" and rejects traversal and malformed values.
func validateRepositoryID(id string) error {
	if strings.Contains(id, "\x00") {
		return fmt.Errorf("invalid repo id %q", id)
	}
	if strings.HasPrefix(id, "/") || strings.HasSuffix(id, "/") {
		return fmt.Errorf("invalid repo id %q", id)
	}
	for _, segment := range strings.Split(id, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("invalid repo id %q", id)
		}
	}
	return nil
}

func sanitizeIDPart(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("value is empty")
	}
	if strings.Contains(value, "/") || strings.Contains(value, "..") || strings.Contains(value, "\x00") {
		return "", fmt.Errorf("value %q contains a path separator", value)
	}
	var b strings.Builder
	prevDash := false
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z', character >= '0' && character <= '9', character == '-', character == '_', character == '.':
			b.WriteRune(character)
			prevDash = false
		case character == ' ' || character == '\t':
			if !prevDash {
				b.WriteRune('-')
				prevDash = true
			}
		default:
			if !prevDash {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "", fmt.Errorf("value %q has no usable characters", value)
	}
	return out, nil
}

func unionKeys(first, second []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, key := range append(append([]string{}, first...), second...) {
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// appendLog persists one plugin log line to the events database with the
// router-owned plugin and type keys attached. All keys are arbitrary
// otherwise and land in event_keys. Severities below the minimum level
// are dropped before persistence.
func (s *Service) appendLog(pluginID, typeKey string, severity int, msg string, keys map[string]string) {
	if slogLevelForSeverity(severity) < s.getMinimumLevel() {
		return
	}
	merged := make(map[string]string, len(keys)+2)
	for name, value := range keys {
		merged[name] = value
	}
	merged["plugin"] = pluginID
	if typeKey != "" {
		merged["type"] = typeKey
	}
	if s.eventsService == nil {
		return
	}
	_ = s.eventsService.Append(context.Background(), time.Now().UTC(), severity, msg, merged)
}

// recordCrash records a PluginInternalError in the bounded per-plugin buffer.
func (s *Service) recordCrash(pluginID, typeKey, cause string) {
	s.logMu.Lock()
	entries := append(s.crashes[pluginID], CrashEntry{At: time.Now(), TypeKey: typeKey, Cause: cause})
	if len(entries) > 50 {
		entries = entries[len(entries)-50:]
	}
	s.crashes[pluginID] = entries
	s.logMu.Unlock()
	if logger := s.getLogger(); logger != nil {
		logger.Debug("Plugin crash recorded", "plugin_id", pluginID, "type", typeKey, "error", cause)
	}
}

// logLookupFailed records a registry miss at debug level with handler
// context. Misses are expected for optional handlers and uninstalled types.
func (s *Service) logLookupFailed(typeKey, handler string, lookupError error) {
	if lookupError == nil {
		return
	}
	if logger := s.getLogger(); logger != nil {
		logger.Debug("Plugin lookup failed", "type", typeKey, "endpoint", handler, "error", lookupError.Error())
	}
}

// Logs returns recent plugin log lines from the events database, newest
// first, capped at 100 rows. It fails when persistence is not wired.
func (s *Service) Logs(ctx context.Context, pluginID string) ([]LogEntry, error) {
	if s.eventsService == nil {
		return nil, fmt.Errorf("plugin log persistence is not configured")
	}
	entries, err := s.eventsService.Query(ctx, events.Query{
		Filters: []events.KeyFilter{{Key: "plugin", Value: &pluginID}},
		Limit:   100,
	})
	if err != nil {
		return nil, err
	}
	out := make([]LogEntry, 0, len(entries))
	for _, entry := range entries {
		keys := make(map[string]string, len(entry.Keys))
		for _, key := range entry.Keys {
			if key.Value != nil {
				keys[key.Key] = *key.Value
			}
		}
		out = append(out, LogEntry{At: entry.Timestamp, Severity: entry.Severity, Message: entry.Message, Keys: keys})
	}
	return out, nil
}

// Crashes returns recent recorded crashes for a plugin, never nil.
func (s *Service) Crashes(pluginID string) []CrashEntry {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	return append([]CrashEntry{}, s.crashes[pluginID]...)
}
