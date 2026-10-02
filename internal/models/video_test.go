package models

import (
	"regexp"
	"testing"
	"time"
)

var videoJobIDPattern = regexp.MustCompile(`^gen-vid-[0-9]+-[0-9A-Za-z]{20}$`)

func TestNewVideoJobID_Format(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 10; i++ {
		id := NewVideoJobID(time.Now())
		if !videoJobIDPattern.MatchString(id) {
			t.Fatalf("job id %q breaks the gen-vid format", id)
		}
		if seen[id] {
			t.Fatalf("duplicate job id %q", id)
		}
		seen[id] = true
	}
}

func TestValidVideoStatus(t *testing.T) {
	for _, s := range []string{"pending", "in_progress", "completed", "failed", "cancelled", "expired"} {
		if !ValidVideoStatus(s) {
			t.Fatalf("status %q must be valid", s)
		}
	}
	for _, s := range []string{"", "queued", "COMPLETED", "done"} {
		if ValidVideoStatus(s) {
			t.Fatalf("status %q must be invalid", s)
		}
	}
}

func TestVideoContentType_Default(t *testing.T) {
	if got := VideoContentType(""); got != "video/mp4" {
		t.Fatalf("default: got %q", got)
	}
	if got := VideoContentType("video/webm"); got != "video/webm" {
		t.Fatalf("passthrough: got %q", got)
	}
}
