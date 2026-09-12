package luaplugin

import (
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	lua "github.com/yuin/gopher-lua"
)

// maxPluginSource caps a single plugin file at 1 MiB.
const maxPluginSource = 1 << 20

// PluginOrigin describes where a plugin was installed from.
type PluginOrigin struct {
	RepoID string `json:"repo_id"`
	Path   string `json:"path"`
	Manual bool   `json:"manual"`
}

// PluginVersionSnapshot is one rollback entry.
type PluginVersionSnapshot struct {
	Version  string              `json:"version"`
	Source   []byte              `json:"source"`
	TypeKeys []string            `json:"type_keys"`
	Handlers map[string][]string `json:"handlers"`
	Icons    map[string]string   `json:"icons"`
}

// PluginRecord is the stored plugin row in BucketPlugins.
type PluginRecord struct {
	ID            string   `json:"id"`
	DisplayName   string   `json:"display_name"`
	Author        string   `json:"author"`
	Version       string   `json:"version"`
	RouterVersion string   `json:"router_version"`
	Description   string   `json:"description"`
	License       string   `json:"license"`
	AllowHosts    []string `json:"allow_hosts"`
	Unsafe        bool     `json:"unsafe"`
	// ProxyLocation and ProxyForceOnMismatch mirror the manifest proxy tags.
	ProxyLocation        string `json:"proxy_location,omitempty"`
	ProxyForceOnMismatch bool   `json:"proxy_force_on_mismatch,omitempty"`
	// ProxySourceKeys lists registered proxy-list sources in this plugin.
	ProxySourceKeys []string                `json:"proxy_source_keys,omitempty"`
	TypeKeys        []string                `json:"type_keys"`
	Handlers        map[string][]string     `json:"handlers"`
	Icons           map[string]string       `json:"icons"`
	Source          []byte                  `json:"source"`
	History         []PluginVersionSnapshot `json:"history"`
	Origin          PluginOrigin            `json:"origin"`
	InstalledAt     time.Time               `json:"installed_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

// LogEntry is one print() line captured from a plugin.
type LogEntry struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

// CrashEntry is one recorded PluginInternalError.
type CrashEntry struct {
	At      time.Time `json:"at"`
	TypeKey string    `json:"type_key"`
	Cause   string    `json:"cause"`
}

// Service owns the plugin lifecycle, the type key registry and handler calls.
type Service struct {
	database *db.DB
	repo     *repository.Repository[PluginRecord]
	storage  *storageBackend
	logger   *slog.Logger

	mu       sync.RWMutex
	registry map[string]*PluginRecord

	logMu   sync.Mutex
	logs    map[string][]LogEntry
	crashes map[string][]CrashEntry

	onChanged func(typeKey string)

	// proxyResolver picks a pool proxy for a plugin call (nil = direct).
	proxyResolver func(rec *PluginRecord, providerConfig map[string]any) (proxyID, proxyURL string, err error)
	// proxyOutcome reports a real request outcome through a proxy.
	proxyOutcome func(proxyID, typeKey string, ok bool, latencyMs int64)
}

// ProxyResolution is the resolver result for one plugin call.
type ProxyResolution struct {
	ProxyID  string
	ProxyURL string
}

// SetProxyResolver wires pool-based proxy selection for plugin HTTP calls.
// A non-nil error fails the handler before any Lua runs (e.g. manual mode
// with no usable proxy, or a geo-forced provider with an empty pool).
func (s *Service) SetProxyResolver(fn func(rec *PluginRecord, providerConfig map[string]any) (proxyID, proxyURL string, err error)) {
	s.proxyResolver = fn
}

// SetProxyOutcomeReporter wires per-provider proxy health feedback.
func (s *Service) SetProxyOutcomeReporter(fn func(proxyID, typeKey string, ok bool, latencyMs int64)) {
	s.proxyOutcome = fn
}

// New loads all enabled plugins into the in-memory registry.
func New(database *db.DB, logger *slog.Logger) (*Service, error) {
	s := &Service{
		database: database,
		repo:     repository.New[PluginRecord](database, db.BucketPlugins, "plugin"),
		storage:  newStorageBackend(database),
		logger:   logger,
		registry: map[string]*PluginRecord{},
		logs:     map[string][]LogEntry{},
		crashes:  map[string][]CrashEntry{},
	}
	if err := s.rebuild(); err != nil {
		return nil, err
	}
	return s, nil
}

// SetLogger wires structured logging.
func (s *Service) SetLogger(l *slog.Logger) { s.logger = l }

// SetOnChanged registers a callback fired with every affected type key
// after install/update/rollback/delete/enable/disable.
func (s *Service) SetOnChanged(fn func(typeKey string)) { s.onChanged = fn }

func (s *Service) notify(keys ...string) {
	if s.onChanged == nil {
		return
	}
	for _, k := range keys {
		s.onChanged(k)
	}
}

func (s *Service) rebuild() error {
	records, err := s.repo.List()
	if err != nil {
		return err
	}
	reg := map[string]*PluginRecord{}
	for _, rec := range records {
		for _, key := range rec.TypeKeys {
			if prev, exists := reg[key]; exists {
				if s.logger != nil {
					s.logger.Warn("duplicate type key across plugins, last wins",
						"type_key", key, "prev", prev.ID, "next", rec.ID)
				}
			}
			cp := *rec
			reg[key] = &cp
		}
	}
	s.mu.Lock()
	s.registry = reg
	s.mu.Unlock()
	return nil
}

// Lookup returns the plugin record serving typeKey.
func (s *Service) Lookup(typeKey string) (*PluginRecord, error) {
	s.mu.RLock()
	rec, ok := s.registry[typeKey]
	s.mu.RUnlock()
	if ok {
		return rec, nil
	}
	return nil, fmt.Errorf("no plugin registered for type key %q", typeKey)
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
func (s *Service) Get(id string) (*PluginRecord, error) {
	return s.repo.Get(id)
}

// List returns all installed plugin records.
func (s *Service) List() ([]*PluginRecord, error) {
	return s.repo.List()
}

// Install validates source and stores it as a new or updated plugin.
// An update pushes the previous version onto History for rollback.
func (s *Service) Install(source []byte, origin PluginOrigin) (*PluginRecord, error) {
	if len(source) == 0 {
		return nil, fmt.Errorf("plugin source is empty")
	}
	if len(source) > maxPluginSource {
		return nil, fmt.Errorf("plugin source exceeds %d bytes", maxPluginSource)
	}
	manifest, err := ParseManifest(source)
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if err := CheckRouterVersion(manifest, models.CurrentVersion); err != nil {
		return nil, err
	}
	id, err := BuildID(origin, manifest)
	if err != nil {
		return nil, err
	}
	typeKeys, handlers, icons, sourceKeys, err := s.dryRun(id, source, manifest)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	existing, err := s.repo.Get(id)
	if err == nil && existing != nil {
		history := append(existing.History, PluginVersionSnapshot{
			Version: existing.Version, Source: existing.Source, TypeKeys: existing.TypeKeys,
			Handlers: existing.Handlers, Icons: existing.Icons,
		})
		if len(history) > 10 {
			history = history[len(history)-10:]
		}
		updated := &PluginRecord{
			ID: id, DisplayName: manifest.Plugin, Author: manifest.Author,
			Version: manifest.Version, RouterVersion: manifest.RouterVersion,
			Description: manifest.Description, License: manifest.License,
			AllowHosts: manifest.AllowHosts, Unsafe: manifest.Unsafe,
			ProxyLocation: manifest.ProxyLocation, ProxyForceOnMismatch: manifest.ProxyForceOnMismatch,
			ProxySourceKeys: sourceKeys,
			TypeKeys:        typeKeys, Handlers: handlers, Icons: icons, Source: append([]byte(nil), source...),
			History: history, Origin: origin,
			InstalledAt: existing.InstalledAt, UpdatedAt: now,
		}
		if err := s.repo.Put(id, updated); err != nil {
			return nil, err
		}
		if err := s.rebuild(); err != nil {
			return nil, err
		}
		s.notify(unionKeys(existing.TypeKeys, typeKeys)...)
		return updated, nil
	}

	rec := &PluginRecord{
		ID: id, DisplayName: manifest.Plugin, Author: manifest.Author,
		Version: manifest.Version, RouterVersion: manifest.RouterVersion,
		Description: manifest.Description, License: manifest.License,
		AllowHosts: manifest.AllowHosts, Unsafe: manifest.Unsafe,
		ProxyLocation: manifest.ProxyLocation, ProxyForceOnMismatch: manifest.ProxyForceOnMismatch,
		ProxySourceKeys: sourceKeys,
		TypeKeys:        typeKeys, Handlers: handlers, Icons: icons, Source: append([]byte(nil), source...),
		Origin:      origin,
		InstalledAt: now, UpdatedAt: now,
	}
	if err := s.repo.Put(id, rec); err != nil {
		return nil, err
	}
	if err := s.rebuild(); err != nil {
		return nil, err
	}
	s.notify(typeKeys...)
	return rec, nil
}

// Rollback restores the most recent previous version. The rolled-back-from
// version is pushed back onto History, so rollback toggles forward again.
func (s *Service) Rollback(id string) (*PluginRecord, error) {
	rec, err := s.repo.Get(id)
	if err != nil {
		return nil, err
	}
	if len(rec.History) == 0 {
		return nil, fmt.Errorf("plugin %q has no previous version", id)
	}
	prev := rec.History[len(rec.History)-1]
	rest := rec.History[:len(rec.History)-1]
	rest = append(rest, PluginVersionSnapshot{Version: rec.Version, Source: rec.Source, TypeKeys: rec.TypeKeys, Handlers: rec.Handlers, Icons: rec.Icons})
	if len(rest) > 10 {
		rest = rest[len(rest)-10:]
	}
	manifest, err := ParseManifest(prev.Source)
	if err != nil {
		return nil, fmt.Errorf("previous version manifest: %w", err)
	}
	handlers := prev.Handlers
	icons := prev.Icons
	if handlers == nil {
		_, handlers, icons, _, err = s.dryRun(id, prev.Source, manifest)
		if err != nil {
			return nil, fmt.Errorf("previous version dry-run: %w", err)
		}
	}
	rec.Version = prev.Version
	rec.Source = prev.Source
	rec.TypeKeys = prev.TypeKeys
	rec.Handlers = handlers
	rec.Icons = icons
	rec.DisplayName = manifest.Plugin
	rec.Author = manifest.Author
	rec.RouterVersion = manifest.RouterVersion
	rec.Description = manifest.Description
	rec.License = manifest.License
	rec.AllowHosts = manifest.AllowHosts
	rec.Unsafe = manifest.Unsafe
	rec.ProxyLocation = manifest.ProxyLocation
	rec.ProxyForceOnMismatch = manifest.ProxyForceOnMismatch
	rec.History = rest
	rec.UpdatedAt = time.Now()
	if err := s.repo.Put(id, rec); err != nil {
		return nil, err
	}
	if err := s.rebuild(); err != nil {
		return nil, err
	}
	s.notify(rec.TypeKeys...)
	return rec, nil
}

// Delete removes a plugin and its storage entries.
func (s *Service) Delete(id string) error {
	rec, err := s.repo.Get(id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	_ = s.storage.deletePlugin(id)
	if err := s.rebuild(); err != nil {
		return err
	}
	s.notify(rec.TypeKeys...)
	return nil
}

// maxPluginIconBytes caps an icon value (data-URI icons live in bbolt).
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

// dryRun executes the plugin top-level code in a fully configured sandbox
// and returns the registered type keys, declared handler names and icons.
// Handlers are not invoked.
func (s *Service) dryRun(pluginID string, source []byte, manifest *Manifest) ([]string, map[string][]string, map[string]string, []string, error) {
	ctx := &execContext{
		pluginID:      pluginID,
		allowHosts:    manifest.AllowHosts,
		unsafe:        manifest.Unsafe,
		logger:        s.logger,
		logSink:       s.appendLog,
		storage:       s.storage,
		registrations: map[string]*lua.LTable{},
		proxySources:  map[string]*lua.LTable{},
	}
	L := newSandboxState(ctx)
	defer L.Close()
	if err := L.DoString(string(source)); err != nil {
		return nil, nil, nil, nil, &models.PluginInternalError{
			PluginID: pluginID, Cause: "top-level: " + luaErrorString(L.Get(-1)),
		}
	}
	if len(ctx.registrations) == 0 && len(ctx.proxySources) == 0 {
		return nil, nil, nil, nil, fmt.Errorf("plugin declares no type keys: missing llm_router.register call")
	}
	keys := make([]string, 0, len(ctx.registrations))
	handlers := map[string][]string{}
	icons := map[string]string{}
	sourceKeys := make([]string, 0, len(ctx.proxySources))
	for k := range ctx.proxySources {
		sourceKeys = append(sourceKeys, k)
	}
	sort.Strings(sourceKeys)
	for k, tbl := range ctx.registrations {
		keys = append(keys, k)
		var names []string
		for _, name := range []string{
			"complete", "complete_stream", "validate_credentials", "get_model_infos",
			"needs_refresh", "refresh_credential", "config_schema",
			"credential_schema", "auth_initiate", "auth_step",
		} {
			if v := tbl.RawGetString(name); v != lua.LNil {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		handlers[k] = names
		if v := tbl.RawGetString("icon"); v != lua.LNil {
			icon, ok := v.(lua.LString)
			if !ok {
				return nil, nil, nil, nil, fmt.Errorf("plugin type %q: icon must be a string", k)
			}
			if err := validateIcon(k, string(icon)); err != nil {
				return nil, nil, nil, nil, err
			}
			if string(icon) != "" {
				icons[k] = string(icon)
			}
		}
	}
	sort.Strings(keys)
	return keys, handlers, icons, sourceKeys, nil
}

// HasHandler reports whether a type key declares a handler.
func (s *Service) HasHandler(typeKey, handler string) bool {
	s.mu.RLock()
	rec, ok := s.registry[typeKey]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	for _, h := range rec.Handlers[typeKey] {
		if h == handler {
			return true
		}
	}
	return false
}

// Icon returns the icon declared by a type key, or empty when none.
func (s *Service) Icon(typeKey string) string {
	s.mu.RLock()
	rec, ok := s.registry[typeKey]
	s.mu.RUnlock()
	if !ok {
		return ""
	}
	return rec.Icons[typeKey]
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
	if err := validateRepoID(origin.RepoID); err != nil {
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

// validateRepoID accepts slash-separated repository IDs such as
// "github/<owner>/<repo>" and rejects traversal and malformed values.
func validateRepoID(id string) error {
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

func sanitizeIDPart(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("value is empty")
	}
	if strings.Contains(s, "/") || strings.Contains(s, "..") || strings.Contains(s, "\x00") {
		return "", fmt.Errorf("value %q contains a path separator", s)
	}
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
			prevDash = false
		case r == ' ' || r == '\t':
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
		return "", fmt.Errorf("value %q has no usable characters", s)
	}
	return out, nil
}

func unionKeys(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range append(append([]string{}, a...), b...) {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// appendLog records a print() line in the bounded per-plugin buffer.
func (s *Service) appendLog(pluginID, msg string) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	entries := append(s.logs[pluginID], LogEntry{At: time.Now(), Message: msg})
	if len(entries) > 100 {
		entries = entries[len(entries)-100:]
	}
	s.logs[pluginID] = entries
}

// recordCrash records a PluginInternalError in the bounded per-plugin buffer.
func (s *Service) recordCrash(pluginID, typeKey, cause string) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	entries := append(s.crashes[pluginID], CrashEntry{At: time.Now(), TypeKey: typeKey, Cause: cause})
	if len(entries) > 50 {
		entries = entries[len(entries)-50:]
	}
	s.crashes[pluginID] = entries
}

// Logs returns recent print() lines for a plugin, never nil.
func (s *Service) Logs(pluginID string) []LogEntry {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	return append([]LogEntry{}, s.logs[pluginID]...)
}

// Crashes returns recent recorded crashes for a plugin, never nil.
func (s *Service) Crashes(pluginID string) []CrashEntry {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	return append([]CrashEntry{}, s.crashes[pluginID]...)
}
