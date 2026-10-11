// Package version owns the Version struct plus Parse/String helpers.
package version

import (
	"fmt"
	"strconv"
	"strings"
)

// Version carries a router or plugin API number.
type Version struct {
	Major int
	Minor int
	Fix   int
}

// Parse builds a Version from "X.Y[.Z]" with an optional "v" prefix.
// "X.Y" reads as Fix 0. Extra whitespace around components is allowed.
func Parse(version string) (Version, error) {
	trimmed := strings.TrimSpace(version)
	trimmed = strings.TrimPrefix(trimmed, "v")
	trimmed = strings.TrimPrefix(trimmed, "V")
	parts := strings.Split(trimmed, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return Version{}, fmt.Errorf("expected MAJOR.MINOR[.FIX], got %q", version)
	}
	numbers := make([]int, 3)
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return Version{}, fmt.Errorf("invalid empty component in %q", version)
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return Version{}, fmt.Errorf("invalid numeric component %q in %q", part, version)
		}
		numbers[i] = number
	}
	return Version{Major: numbers[0], Minor: numbers[1], Fix: numbers[2]}, nil
}

// String renders "X.Y" when Fix is 0, otherwise "X.Y.Z".
func (v Version) String() string {
	if v.Fix == 0 {
		return fmt.Sprintf("%d.%d", v.Major, v.Minor)
	}
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Fix)
}
