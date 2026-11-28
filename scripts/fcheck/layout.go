package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// layoutRule is one way the frontend could place a view by number instead of by role. FUI owns
// spacing: its containers inset their content and its stacks space their children by role
// (docs/LAYOUT.md in FUI), so a page that reaches for a number works around a missing default.
type layoutRule struct {
	pattern *regexp.Regexp
	// what to use instead
	message string
	// file extensions the rule reads
	extensions []string
}

var layoutRules = []layoutRule{
	{
		pattern:    regexp.MustCompile(`\s(gap|pad|padX|padY)=`),
		message:    "FUI removed gap and pad: use spacing=\"tight|default|group|section|none\" or padding",
		extensions: []string{".svelte"},
	},
	{
		pattern:    regexp.MustCompile(`\s(truncate(\s|=|/?>)|lines=|break(\s*/?>|\s+\w+=))`),
		message:    "Text dropped truncate, lines and break: use lineLimit and truncationMode; text wraps by itself",
		extensions: []string{".svelte"},
	},
	{
		pattern:    regexp.MustCompile(`--fui-space-\d`),
		message:    "the spacing ladder is FUI's own geometry: use a --fui-spacing-* role or --fui-content-margin",
		extensions: []string{".svelte", ".css", ".ts"},
	},
	{
		pattern:    regexp.MustCompile(`(padding|margin|gap)[a-z-]*\s*:[^;]*\d(px|rem|em)`),
		message:    "a spacing literal: use a --fui-spacing-* role or --fui-content-margin",
		extensions: []string{".svelte", ".css"},
	},
}

// layoutViolation is one line that breaks a layout rule.
type layoutViolation struct {
	path    string
	line    int
	message string
}

func (violation layoutViolation) String() string {
	return fmt.Sprintf("%s:%d: %s", violation.path, violation.line, violation.message)
}

// checkLayout reads every source file under root and returns the lines that place views by number.
func checkLayout(root string) ([]layoutViolation, error) {
	var violations []layoutViolation
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() {
			return nil
		}
		content, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		relative, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return relativeError
		}
		violations = append(violations, layoutViolationsOf(filepath.ToSlash(relative), string(content))...)
		return nil
	})
	return violations, err
}

// layoutViolationsOf checks one file's content against the rules that read its extension.
func layoutViolationsOf(path string, content string) []layoutViolation {
	extension := filepath.Ext(path)
	var violations []layoutViolation
	for index, line := range strings.Split(content, "\n") {
		for _, rule := range layoutRules {
			if !hasExtension(rule.extensions, extension) || !rule.pattern.MatchString(line) {
				continue
			}
			violations = append(violations, layoutViolation{path: path, line: index + 1, message: rule.message})
		}
	}
	return violations
}

func hasExtension(extensions []string, extension string) bool {
	for _, candidate := range extensions {
		if candidate == extension {
			return true
		}
	}
	return false
}
