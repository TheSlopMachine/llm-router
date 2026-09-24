package shared

import (
	"reflect"
	"testing"
)

func TestTailLines(t *testing.T) {
	cases := []struct {
		name string
		data string
		n    int
		want []string
	}{
		{"empty", "", 10, nil},
		{"newline only", "\n", 10, nil},
		{"fewer than n", "a\nb\n", 10, []string{"a", "b"}},
		{"exact", "a\nb\nc", 3, []string{"a", "b", "c"}},
		{"more than n", "a\nb\nc\nd\n", 2, []string{"c", "d"}},
		{"no trailing newline", "a\nb", 2, []string{"a", "b"}},
		{"zero means all", "a\nb\nc\n", 0, []string{"a", "b", "c"}},
		{"negative means all", "a\nb\n", -5, []string{"a", "b"}},
		{"single line", "only\n", 10, []string{"only"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TailLines([]byte(tc.data), tc.n); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLinesFromEnv(t *testing.T) {
	t.Setenv("LINES", "")
	if got := LinesFromEnv(100); got != 100 {
		t.Errorf("unset: got %d", got)
	}
	t.Setenv("LINES", "20")
	if got := LinesFromEnv(100); got != 20 {
		t.Errorf("valid: got %d", got)
	}
	for _, bad := range []string{"0", "-3", "abc", "12x"} {
		t.Setenv("LINES", bad)
		if got := LinesFromEnv(100); got != 100 {
			t.Errorf("%q: got %d, want default", bad, got)
		}
	}
}

func TestFollowFromEnv(t *testing.T) {
	for _, on := range []string{"1", "true", "yes", "on", " TRUE "} {
		t.Setenv("FOLLOW", on)
		if !FollowFromEnv() {
			t.Errorf("%q: want follow", on)
		}
	}
	for _, off := range []string{"0", "false", "no", "off", " False "} {
		t.Setenv("FOLLOW", off)
		if FollowFromEnv() {
			t.Errorf("%q: want no follow", off)
		}
	}
}
