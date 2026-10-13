package shared

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Sentinel errors for path expansion. Match with errors.Is.
var (
	ErrHomeDirectoryUnresolvable = errors.New("home directory unresolvable")
	ErrUnsupportedTilde          = errors.New("unsupported tilde expression")
)

// ExpandPath resolves a leading tilde, Windows %NAME% references and POSIX
// $VAR/${VAR} references in path, then cleans the result.
//
// Tilde expansion is platform-agnostic and applies only at position zero:
// "~" alone returns the home directory, "~/" and "~\" join the home
// directory with the remainder. "~other/..." returns ErrUnsupportedTilde.
// A tilde elsewhere ("/test/~") stays literal. Tilde expansion runs once
// before variable expansion with no re-scan, so values introduced by
// variables never gain tilde treatment.
//
// Percent and dollar expansion run on every OS. "%NAME%" with
// NAME=[A-Za-z0-9_]+ resolves through the process environment (missing
// resolves to empty, "%%" resolves to "%", a lone "%" stays literal).
// "$VAR" and "${VAR}" resolve through os.ExpandEnv (missing resolves to
// empty).
//
// Empty input returns empty without error. Non-empty results return with
// filepath.Clean applied; Windows drive, UNC and backslash forms survive
// because Clean is OS-aware.
//
// Mirror: internal/config/expand.go carries the same semantics for the
// server module. The two files stay in sync; stability tests in both
// modules lock the behavior.
func ExpandPath(path string) (string, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return "", nil
	}
	if strings.HasPrefix(trimmedPath, "~") {
		return expandTildePath(trimmedPath)
	}
	expandedValue := expandVariables(trimmedPath)
	if strings.TrimSpace(expandedValue) == "" {
		return "", nil
	}
	return filepath.Clean(expandedValue), nil
}

// GetenvPath returns the expanded value of name, or the expanded fallback
// when unset or blank. Expansion failures stop the helper process.
func GetenvPath(name, fallback string) string {
	value := Getenv(name, fallback)
	expandedValue, err := ExpandPath(value)
	if err != nil {
		Failf("invalid %s: %v", name, err)
	}
	return expandedValue
}

// expandTildePath resolves trimmedPath starting with "~" against the home
// directory. Only "~", "~/" and "~\" forms expand.
func expandTildePath(trimmedPath string) (string, error) {
	homeDirectory, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(homeDirectory) == "" {
		return "", fmt.Errorf("expand path %q: %w", trimmedPath, ErrHomeDirectoryUnresolvable)
	}
	if len(trimmedPath) == 1 {
		return filepath.Clean(homeDirectory), nil
	}
	separator := trimmedPath[1]
	if separator != '/' && separator != '\\' {
		return "", fmt.Errorf("expand path %q: %w", trimmedPath, ErrUnsupportedTilde)
	}
	remainder := trimmedPath[2:]
	if strings.TrimSpace(remainder) == "" {
		return filepath.Clean(homeDirectory), nil
	}
	expandedRemainder := expandVariables(remainder)
	if strings.TrimSpace(expandedRemainder) == "" {
		return filepath.Clean(homeDirectory), nil
	}
	return filepath.Join(homeDirectory, expandedRemainder), nil
}

// expandVariables resolves %NAME% references first, then $VAR/${VAR}.
func expandVariables(value string) string {
	return os.ExpandEnv(expandPercentVariables(value))
}

// expandPercentVariables resolves %NAME% references through the process
// environment. Pairs with an empty or invalid name stay literal except
// "%%", which resolves to "%". A "%" without a closing "%" stays literal.
func expandPercentVariables(value string) string {
	var builder strings.Builder
	position := 0
	for position < len(value) {
		relativePercent := strings.IndexByte(value[position:], '%')
		if relativePercent < 0 {
			builder.WriteString(value[position:])
			break
		}
		percentPosition := position + relativePercent
		builder.WriteString(value[position:percentPosition])
		relativeClosing := strings.IndexByte(value[percentPosition+1:], '%')
		if relativeClosing < 0 {
			builder.WriteString(value[percentPosition:])
			break
		}
		closingPosition := percentPosition + 1 + relativeClosing
		variableName := value[percentPosition+1 : closingPosition]
		if variableName == "" {
			builder.WriteByte('%')
			position = closingPosition + 1
			continue
		}
		if !isPercentVariableName(variableName) {
			builder.WriteString(value[percentPosition : closingPosition+1])
			position = closingPosition + 1
			continue
		}
		builder.WriteString(os.Getenv(variableName))
		position = closingPosition + 1
	}
	return builder.String()
}

// isPercentVariableName reports whether name fits [A-Za-z0-9_]+.
func isPercentVariableName(name string) bool {
	if len(name) == 0 {
		return false
	}
	for _, character := range name {
		switch {
		case character >= 'A' && character <= 'Z':
		case character >= 'a' && character <= 'z':
		case character >= '0' && character <= '9':
		case character == '_':
		default:
			return false
		}
	}
	return true
}
