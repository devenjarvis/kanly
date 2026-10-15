package schema

import "testing"

func TestRaiseFileGoVersion(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"no constraint", "package p\n", "package p\n"},
		{"old version raised", "//go:build go1.6\n// +build go1.6\n\npackage p\n", "//go:build go1.21\n// +build go1.6\n\npackage p\n"},
		{"inside expression", "//go:build go1.16 && !appengine\n\npackage p\n", "//go:build go1.21 && !appengine\n\npackage p\n"},
		{"negated old version raised", "//go:build !go1.9 || linux\n\npackage p\n", "//go:build !go1.21 || linux\n\npackage p\n"},
		{"new enough left alone", "//go:build go1.22\n\npackage p\n", "//go:build go1.22\n\npackage p\n"},
		{"non-version tags left alone", "//go:build linux && amd64\n\npackage p\n", "//go:build linux && amd64\n\npackage p\n"},
		{"after package clause ignored", "package p\n\n//go:build go1.6\n", "package p\n\n//go:build go1.6\n"},
		{"CRLF line ending kept", "//go:build go1.6\r\n\r\npackage p\r\n", "//go:build go1.21\r\n\r\npackage p\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := raiseFileGoVersion(tt.in); got != tt.want {
				t.Errorf("raiseFileGoVersion(%q) =\n%q\nwant\n%q", tt.in, got, tt.want)
			}
		})
	}
}
