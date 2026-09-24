package util

import "testing"

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"My Helper", "my-helper"},
		{"under_score", "under-score"},
		{"  spaced  ", "spaced"},
		{"UPPER", "upper"},
		{"a/b:c", "abc"},
		{"---", ""},
		{"", ""},
		{"gpt-4o", "gpt-4o"},
		{"a  b__c", "a-b-c"},
	}
	for _, tc := range cases {
		if got := Slugify(tc.in); got != tc.want {
			t.Errorf("Slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
