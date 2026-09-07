package luaplugin

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed bundled/*.lua
var bundledFS embed.FS

// BundledPlugin describes one embedded plugin file.
type BundledPlugin struct {
	Filename string
	Source   []byte
	Manifest *Manifest
}

// BundledPlugins reads all embedded plugin files with parsed manifests.
func BundledPlugins() ([]BundledPlugin, error) {
	entries, err := bundledFS.ReadDir("bundled")
	if err != nil {
		return nil, err
	}
	var out []BundledPlugin
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lua") {
			continue
		}
		source, err := bundledFS.ReadFile("bundled/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read bundled %s: %w", e.Name(), err)
		}
		manifest, err := ParseManifest(source)
		if err != nil {
			return nil, fmt.Errorf("bundled %s manifest: %w", e.Name(), err)
		}
		out = append(out, BundledPlugin{Filename: e.Name(), Source: source, Manifest: manifest})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Filename < out[j].Filename })
	return out, nil
}

// EnsureBundled installs embedded plugins on first run and upgrades them
// when the embedded version is newer. User-disabled state is preserved:
// brand-new installs are enabled, upgrades keep the existing flag.
func (s *Service) EnsureBundled() error {
	bundled, err := BundledPlugins()
	if err != nil {
		return err
	}
	for _, b := range bundled {
		origin := PluginOrigin{RepoID: "bundled", Path: "llm-router-plugins/" + b.Filename}
		id, err := BuildID(origin, b.Manifest)
		if err != nil {
			return fmt.Errorf("bundled %s: %w", b.Filename, err)
		}
		existing, err := s.repo.Get(id)
		if err == nil && existing != nil {
			cmp, cerr := CompareVersions(b.Manifest.Version, existing.Version)
			if cerr != nil || cmp <= 0 {
				continue
			}
			if _, ierr := s.Install(b.Source, origin); ierr != nil {
				return fmt.Errorf("upgrade bundled %s: %w", b.Filename, ierr)
			}
			continue
		}
		if _, ierr := s.Install(b.Source, origin); ierr != nil {
			return fmt.Errorf("install bundled %s: %w", b.Filename, ierr)
		}
	}
	return nil
}
