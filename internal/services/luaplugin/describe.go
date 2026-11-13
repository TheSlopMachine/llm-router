package luaplugin

import (
	"crypto/sha256"
	"sort"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// Subsystem names identify the router surface a plugin extends.
const (
	SubsystemProviders = "providers"
	SubsystemProxies   = "proxies"
)

// maxDescriptionEntries bounds the in-memory description cache.
const maxDescriptionEntries = 256

// Description is the registration surface of one plugin source resolved
// without installing it.
type Description struct {
	// Subsystems lists the surfaces the plugin registers, never nil.
	Subsystems []string
	// Icon is the first declared icon by type key order, or empty.
	Icon string
}

type descriptionEntry struct {
	description Description
	err         error
}

// descriptionCache memoizes Describe per source digest.
type descriptionCache struct {
	mutex   sync.Mutex
	entries map[[sha256.Size]byte]descriptionEntry
}

func subsystemsOf(typeKeys, sourceKeys []string) []string {
	subsystems := []string{}
	if len(typeKeys) > 0 {
		subsystems = append(subsystems, SubsystemProviders)
	}
	if len(sourceKeys) > 0 {
		subsystems = append(subsystems, SubsystemProxies)
	}
	return subsystems
}

// firstIcon returns the icon of the lowest type key that declares one.
func firstIcon(icons map[string]string) string {
	keys := make([]string, 0, len(icons))
	for key := range icons {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if icons[key] != "" {
			return icons[key]
		}
	}
	return ""
}

// Subsystems lists the router surfaces the plugin registers.
func (record *PluginRecord) Subsystems() []string {
	return subsystemsOf(record.TypeKeys, record.ProxySourceKeys)
}

// Icon returns the icon declared by the plugin, or empty when none.
func (record *PluginRecord) Icon() string {
	return firstIcon(record.Icons)
}

// Describe runs the top-level code of a plugin source that is not installed
// and returns its subsystems and icon. The run has no storage and no log
// sink and is bound by dryRunTimeout. Results, including failures, are
// cached by source digest.
func (s *Service) Describe(source []byte, manifest *Manifest) (Description, error) {
	digest := sha256.Sum256(source)
	s.descriptions.mutex.Lock()
	entry, cached := s.descriptions.entries[digest]
	s.descriptions.mutex.Unlock()
	if cached {
		return entry.description, entry.err
	}
	ctx := &executionContext{
		pluginID:      "describe",
		allowHosts:    manifest.AllowHosts,
		unsafe:        manifest.Unsafe,
		logger:        s.getLogger(),
		registrations: map[string]*lua.LTable{},
		proxySources:  map[string]*lua.LTable{},
	}
	result, err := evaluateRegistrations(ctx, source)
	entry = descriptionEntry{err: err}
	if err == nil {
		entry.description = Description{
			Subsystems: subsystemsOf(result.keys, result.sourceKeys),
			Icon:       firstIcon(result.icons),
		}
	}
	s.descriptions.mutex.Lock()
	if s.descriptions.entries == nil || len(s.descriptions.entries) >= maxDescriptionEntries {
		s.descriptions.entries = map[[sha256.Size]byte]descriptionEntry{}
	}
	s.descriptions.entries[digest] = entry
	s.descriptions.mutex.Unlock()
	return entry.description, entry.err
}
