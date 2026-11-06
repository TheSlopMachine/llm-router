package datamanagement

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	stdErrors "errors"

	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
)

// The plugins subsystem travels as a zip archive: PluginsArchiveSettingsFile
// holds the PluginsExportBundle with every plugin's source and version
// history removed, and every plugin's source is a separate
// PluginSourceFileName entry.
const (
	PluginsArchiveSettingsFile = "plugins.json"
	// MaxPluginsArchiveEntryBytes caps one decompressed archive entry.
	MaxPluginsArchiveEntryBytes = 8 << 20
)

// ErrInvalidPluginsArchive marks an archive the plugins import cannot read.
var ErrInvalidPluginsArchive = stdErrors.New("invalid plugins archive")

// PluginSourceFileName is the archive entry and download file name of a
// plugin's source: the plugin ID plus ".lua". IDs hold one slash
// (author/plugin), so inside the archive the source sits in a folder named
// after the author.
func PluginSourceFileName(pluginID string) string {
	return pluginID + ".lua"
}

// WritePluginsArchive writes the bundle as a plugins zip archive.
func WritePluginsArchive(writer io.Writer, bundle *PluginsExportBundle) error {
	archive := zip.NewWriter(writer)
	settings := PluginsExportBundle{
		Repos:   bundle.Repos,
		Plugins: make([]*luaplugin.PluginRecord, 0, len(bundle.Plugins)),
	}
	for _, plugin := range bundle.Plugins {
		if plugin == nil {
			continue
		}
		entry, err := archive.Create(PluginSourceFileName(plugin.ID))
		if err != nil {
			return err
		}
		if _, err := entry.Write(plugin.Source); err != nil {
			return err
		}
		metadata := *plugin
		metadata.Source = nil
		metadata.History = nil
		settings.Plugins = append(settings.Plugins, &metadata)
	}
	entry, err := archive.Create(PluginsArchiveSettingsFile)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(entry)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(settings); err != nil {
		return err
	}
	return archive.Close()
}

// ReadPluginsArchive reads a plugins zip archive back into a bundle: the
// settings file plus each listed plugin's source. A missing settings file or
// a listed plugin without its source entry fails the whole archive. Entries
// the settings file does not list are ignored.
func ReadPluginsArchive(data []byte) (*PluginsExportBundle, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPluginsArchive, err)
	}
	entries := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		entries[file.Name] = file
	}
	settingsFile, ok := entries[PluginsArchiveSettingsFile]
	if !ok {
		return nil, fmt.Errorf("%w: %s is missing", ErrInvalidPluginsArchive, PluginsArchiveSettingsFile)
	}
	settings, err := readArchiveEntry(settingsFile)
	if err != nil {
		return nil, err
	}
	var bundle PluginsExportBundle
	if err := json.Unmarshal(settings, &bundle); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidPluginsArchive, PluginsArchiveSettingsFile, err)
	}
	for _, plugin := range bundle.Plugins {
		if plugin == nil {
			continue
		}
		name := PluginSourceFileName(plugin.ID)
		sourceFile, ok := entries[name]
		if !ok {
			return nil, fmt.Errorf("%w: %s is missing", ErrInvalidPluginsArchive, name)
		}
		source, err := readArchiveEntry(sourceFile)
		if err != nil {
			return nil, err
		}
		plugin.Source = source
	}
	return &bundle, nil
}

func readArchiveEntry(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidPluginsArchive, file.Name, err)
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, MaxPluginsArchiveEntryBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidPluginsArchive, file.Name, err)
	}
	if len(content) > MaxPluginsArchiveEntryBytes {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrInvalidPluginsArchive, file.Name, MaxPluginsArchiveEntryBytes)
	}
	return content, nil
}
