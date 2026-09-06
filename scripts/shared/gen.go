package shared

import (
	"bytes"
	"fmt"
	"os"
)

// WriteIfChanged writes data to path only if the content differs.
// Returns true when the file was written.
func WriteIfChanged(path string, data []byte, perm os.FileMode) (bool, error) {
	old, err := os.ReadFile(path)
	if err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}
