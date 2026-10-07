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
func Parse(s string) (Version, error) {
	t := strings.TrimSpace(s)
	t = strings.TrimPrefix(t, "v")
	t = strings.TrimPrefix(t, "V")
	parts := strings.Split(t, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return Version{}, fmt.Errorf("expected MAJOR.MINOR[.FIX], got %q", s)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return Version{}, fmt.Errorf("invalid empty component in %q", s)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("invalid numeric component %q in %q", p, s)
		}
		nums[i] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Fix: nums[2]}, nil
}

// String renders "X.Y" when Fix is 0, otherwise "X.Y.Z".
func (v Version) String() string {
	if v.Fix == 0 {
		return fmt.Sprintf("%d.%d", v.Major, v.Minor)
	}
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Fix)
}
