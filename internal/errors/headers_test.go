package errors

import (
	"net/http/httptest"
	"testing"
)

func TestSanitizeResponseHeaders(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]string
		want map[string]string
	}{
		{"nil", nil, nil},
		{"empty", map[string]string{}, nil},
		{"seconds", map[string]string{"Retry-After": "3"}, map[string]string{"Retry-After": "3"}},
		{"zero", map[string]string{"Retry-After": "0"}, map[string]string{"Retry-After": "0"}},
		{"date", map[string]string{"Retry-After": "Wed, 21 Oct 2015 07:28:00 GMT"},
			map[string]string{"Retry-After": "Wed, 21 Oct 2015 07:28:00 GMT"}},
		{"case", map[string]string{"retry-after": "30"}, map[string]string{"Retry-After": "30"}},
		{"negative", map[string]string{"Retry-After": "-1"}, nil},
		{"garbage", map[string]string{"Retry-After": "soon"}, nil},
		{"empty value", map[string]string{"Retry-After": ""}, nil},
		{"disallowed", map[string]string{"Content-Length": "3"}, nil},
		{"mixed", map[string]string{"Retry-After": "3", "Connection": "close"},
			map[string]string{"Retry-After": "3"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeResponseHeaders(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestWriteResponseHeaders(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteResponseHeaders(rr, map[string]string{"Retry-After": "30", "X-Drop": "1"})
	if got := rr.Header().Get("Retry-After"); got != "30" {
		t.Fatalf("Retry-After = %q, want %q", got, "30")
	}
	if got := rr.Header().Get("X-Drop"); got != "" {
		t.Fatalf("non-allow-listed header leaked, got %q", got)
	}
	WriteResponseHeaders(rr, nil)
}
