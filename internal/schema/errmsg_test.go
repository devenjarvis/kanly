package schema

import "testing"

func TestWrapsError(t *testing.T) {
	for _, tc := range []struct {
		format string
		want   bool
	}{
		{"%w", true},
		{"op: %w", true},
		{"%[2]w", true},
		{"%-5w", true},
		{"%v", false},
		{"%%w", false},
		{"%%%w", true},
		{"50%", false},
		{"%", false},
		{"", false},
		{"w", false},
	} {
		if got := wrapsError(tc.format); got != tc.want {
			t.Errorf("wrapsError(%q) = %v, want %v", tc.format, got, tc.want)
		}
	}
}
