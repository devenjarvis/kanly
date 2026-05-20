package runner

import (
	"reflect"
	"testing"
)

func TestParseCoverProfile(t *testing.T) {
	const profile = `mode: set
multipkg/foo/foo.go:3.30,3.43 1 1
multipkg/foo/foo.go:5.30,5.43 1 0
multipkg/foo/bar.go:10.1,12.20 2 3
`
	got, err := parseCoverProfile([]byte(profile))
	if err != nil {
		t.Fatalf("parseCoverProfile: %v", err)
	}

	want := []coverBlock{
		{File: "multipkg/foo/foo.go", StartLine: 3, EndLine: 3, Count: 1},
		{File: "multipkg/foo/bar.go", StartLine: 10, EndLine: 12, Count: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsed blocks mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestParseCoverProfileMissingHeader(t *testing.T) {
	_, err := parseCoverProfile([]byte("foo.go:1.1,1.2 1 1\n"))
	if err == nil {
		t.Fatal("expected error for missing header")
	}
}

func TestParseCoverProfileEmptyBody(t *testing.T) {
	got, err := parseCoverProfile([]byte("mode: set\n"))
	if err != nil {
		t.Fatalf("parseCoverProfile: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want 0 blocks, got %d", len(got))
	}
}
