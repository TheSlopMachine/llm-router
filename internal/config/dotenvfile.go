package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Source names where an effective setting value comes from.
type Source string

const (
	SourceEnvironment Source = "env"
	SourceFile        Source = "file"
	SourceDefault     Source = "default"
)

// Setting is one key with its effective value and the place it comes from.
type Setting struct {
	Key    Key
	Value  string
	Source Source
}

// DotenvFile edits one dotenv file line by line. Comments, blank lines and
// the order of untouched assignments survive every edit.
type DotenvFile struct {
	Path string
}

// Lookup returns the last assignment of name. Found is false when the file
// or the assignment is missing.
func (file DotenvFile) Lookup(name string) (string, bool, error) {
	lines, err := file.readLines()
	if err != nil {
		return "", false, err
	}
	value, found := lastAssignment(lines, name)
	return value, found, nil
}

// Set replaces the last assignment of name or appends one. A missing file
// starts from the default template.
func (file DotenvFile) Set(name, value string) error {
	assignment, err := formatAssignment(name, value)
	if err != nil {
		return err
	}
	lines, err := file.readLines()
	if err != nil {
		return err
	}
	if lines == nil {
		lines = templateLines()
	}
	replaced := false
	for index := len(lines) - 1; index >= 0; index-- {
		if assignmentName(lines[index]) == name {
			lines[index] = assignment
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, assignment)
	}
	return file.writeLines(lines)
}

// Unset removes every assignment of name and reports whether one existed.
// A missing file is not an error.
func (file DotenvFile) Unset(name string) (bool, error) {
	lines, err := file.readLines()
	if err != nil {
		return false, err
	}
	kept := make([]string, 0, len(lines))
	removed := false
	for _, line := range lines {
		if assignmentName(line) == name {
			removed = true
			continue
		}
		kept = append(kept, line)
	}
	if !removed {
		return false, nil
	}
	return true, file.writeLines(kept)
}

// ResolveSettings returns every key with its effective value. Process
// environment wins over the file, the file wins over the default. Blank
// values count as unset, matching Load.
func ResolveSettings(path string) ([]Setting, error) {
	file := DotenvFile{Path: path}
	lines, err := file.readLines()
	if err != nil {
		return nil, err
	}
	settings := make([]Setting, 0, len(keys))
	for _, key := range keys {
		setting := Setting{Key: key, Value: runtimeDefault(key), Source: SourceDefault}
		if value := strings.TrimSpace(os.Getenv(key.Name)); value != "" {
			setting.Value, setting.Source = value, SourceEnvironment
		} else if value, found := lastAssignment(lines, key.Name); found && strings.TrimSpace(value) != "" {
			setting.Value, setting.Source = strings.TrimSpace(value), SourceFile
		}
		settings = append(settings, setting)
	}
	return settings, nil
}

// runtimeDefault returns the value Load uses when nothing is set. The path
// defaults honor the XDG variables, while Key.Default stays the portable
// text of the template.
func runtimeDefault(key Key) string {
	switch key.Name {
	case "DB_PATH":
		return defaultDatabasePath()
	case "CACHE_PATH":
		return defaultCachePath()
	case "EVENTS_PATH":
		return defaultEventsPath()
	}
	return key.Default
}

// ReplaceDefaultFile writes DefaultTemplate to target and overwrites an
// existing file. Managed parent directories are created.
func ReplaceDefaultFile(target string) error {
	cleaned := strings.TrimSpace(target)
	if cleaned == "" {
		return fmt.Errorf("configuration target path is required: %w", ErrMissingTargetPath)
	}
	expandedTarget, err := ExpandPath(cleaned)
	if err != nil {
		return fmt.Errorf("invalid configuration target %s: %w", cleaned, err)
	}
	if err := EnsureParentDirectory(expandedTarget); err != nil {
		return fmt.Errorf("invalid configuration target %s: %w", cleaned, err)
	}
	return writeFileAtomically(expandedTarget, []byte(DefaultTemplate()))
}

func (file DotenvFile) readLines() ([]string, error) {
	raw, err := os.ReadFile(file.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dotenv file %s: %w", file.Path, err)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return []string{}, nil
	}
	return strings.Split(text, "\n"), nil
}

func (file DotenvFile) writeLines(lines []string) error {
	if err := EnsureParentDirectory(file.Path); err != nil {
		return err
	}
	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	return writeFileAtomically(file.Path, []byte(content))
}

func templateLines() []string {
	return strings.Split(strings.TrimSuffix(DefaultTemplate(), "\n"), "\n")
}

// writeFileAtomically replaces path through a temporary sibling so a crash
// never leaves a half-written file. Symlinks are resolved first and the
// existing permission bits are kept.
func writeFileAtomically(path string, content []byte) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".env-*")
	if err != nil {
		return fmt.Errorf("create temporary file next to %s: %w", target, err)
	}
	temporaryPath := temporary.Name()
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("write %s: %w", temporaryPath, err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("close %s: %w", temporaryPath, err)
	}
	if err := os.Chmod(temporaryPath, mode); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("chmod %s: %w", temporaryPath, err)
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("replace %s: %w", target, err)
	}
	return nil
}

func lastAssignment(lines []string, name string) (string, bool) {
	for index := len(lines) - 1; index >= 0; index-- {
		if assignmentName(lines[index]) == name {
			_, value, _ := parseAssignment(lines[index])
			return value, true
		}
	}
	return "", false
}

func assignmentName(line string) string {
	name, _, ok := parseAssignment(line)
	if !ok {
		return ""
	}
	return name
}

// parseAssignment splits one dotenv line. Comments, blank lines and lines
// without a name before "=" are not assignments.
func parseAssignment(line string) (name, value string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	trimmed = strings.TrimPrefix(trimmed, "export ")
	separator := strings.Index(trimmed, "=")
	if separator <= 0 {
		return "", "", false
	}
	name = strings.TrimSpace(trimmed[:separator])
	if name == "" || strings.ContainsAny(name, " \t") {
		return "", "", false
	}
	return name, unquoteValue(strings.TrimSpace(trimmed[separator+1:])), true
}

func unquoteValue(raw string) string {
	if raw == "" {
		return ""
	}
	switch raw[0] {
	case '\'':
		if end := strings.Index(raw[1:], "'"); end >= 0 {
			return raw[1 : 1+end]
		}
	case '"':
		var builder strings.Builder
		for index := 1; index < len(raw); index++ {
			character := raw[index]
			if character == '\\' && index+1 < len(raw) {
				index++
				switch raw[index] {
				case 'n':
					builder.WriteByte('\n')
				default:
					builder.WriteByte(raw[index])
				}
				continue
			}
			if character == '"' {
				return builder.String()
			}
			builder.WriteByte(character)
		}
	}
	if comment := strings.Index(raw, " #"); comment >= 0 {
		raw = raw[:comment]
	}
	return strings.TrimSpace(raw)
}

func formatAssignment(name, value string) (string, error) {
	if strings.ContainsAny(value, "'\n\r") {
		return "", fmt.Errorf("value for %s must not contain quotes or line breaks", name)
	}
	if strings.ContainsAny(value, " \t#\"\\") {
		value = "'" + value + "'"
	}
	return name + "=" + value, nil
}
