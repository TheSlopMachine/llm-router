package shared

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// followPollInterval paces the follow loop: tail -f semantics without
// inotify, portable across Windows and unix.
const followPollInterval = 250 * time.Millisecond

// TailLines returns the last tailSize lines of data, split without the trailing
// newline. tailSize <= 0 means all lines. Empty input yields no lines.
func TailLines(data []byte, tailSize int) []string {
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if tailSize <= 0 || tailSize >= len(lines) {
		return lines
	}
	return lines[len(lines)-tailSize:]
}

// LinesFromEnv reads the LINES tail size: positive integer, default
// defaultLines on unset, empty, or invalid values.
func LinesFromEnv(defaultLines int) int {
	raw := strings.TrimSpace(os.Getenv("LINES"))
	if raw == "" {
		return defaultLines
	}
	number, err := strconv.Atoi(raw)
	if err != nil || number <= 0 {
		return defaultLines
	}
	return number
}

// FollowFromEnv resolves whether to follow: FOLLOW=1/true/yes/on forces
// follow, FOLLOW=0/false/no/off forces dump-and-exit, unset auto-detects
// from stdout (follow only on a TTY, so piped agent calls never block).
func FollowFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FOLLOW"))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return IsTTY()
	}
}

// IsTTY reports whether stdout is a character device (interactive
// terminal) rather than a pipe or file.
func IsTTY() bool {
	fileInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fileInfo.Mode()&os.ModeCharDevice != 0
}

// DumpAndFollow prints the last lines of path, then follows appends like
// tail -f when follow is set. A shrink (log truncated by a restart) resets
// to the new end with a marker instead of replaying or stalling.
func DumpAndFollow(path string, lines int, follow bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read log %s: %w (is the dev server running? see make status)", path, err)
	}
	for _, line := range TailLines(data, lines) {
		fmt.Println(line)
	}
	if !follow {
		return nil
	}
	offset := int64(len(data))
	for {
		time.Sleep(followPollInterval)
		fileInfo, err := os.Stat(path)
		if err != nil {
			continue
		}
		size := fileInfo.Size()
		if size < offset {
			offset = size
			fmt.Println("-- log truncated, following from end --")
			continue
		}
		if size == offset {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		if _, err := file.Seek(offset, 0); err != nil {
			file.Close()
			continue
		}
		buffer := make([]byte, size-offset)
		bytesRead, _ := file.Read(buffer)
		file.Close()
		offset += int64(bytesRead)
		fmt.Print(string(buffer[:bytesRead]))
	}
}
