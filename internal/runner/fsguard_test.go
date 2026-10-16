package runner_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/devenjarvis/kanly/internal/runner"
)

func TestDirSnapshotRestore(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package a")
	write("testdata/fixture.txt", "fixture")
	write("sub/other.go", "package sub") // another package: not guarded

	snap, err := runner.SnapshotDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	write("a.go", "package a // changed")
	write("out.txt", "created by a test")
	write("testdata/new/deep.txt", "created")
	write("testdata/golden.txt", "created")
	write("sub/created.txt", "outside the guard")
	if err := os.Remove(filepath.Join(dir, "testdata/fixture.txt")); err != nil {
		t.Fatal(err)
	}

	removed, changed, err := snap.Restore()
	if err != nil {
		t.Fatal(err)
	}
	abs := func(rels ...string) []string {
		var out []string
		for _, r := range rels {
			out = append(out, filepath.Join(dir, r))
		}
		return out
	}
	if want := abs("out.txt", "testdata/golden.txt", "testdata/new"); !reflect.DeepEqual(removed, want) {
		t.Errorf("removed = %v, want %v", removed, want)
	}
	if want := abs("a.go", "testdata/fixture.txt"); !reflect.DeepEqual(changed, want) {
		t.Errorf("changed = %v, want %v", changed, want)
	}
	for _, p := range abs("out.txt", "testdata/new", "testdata/golden.txt") {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists", p)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "sub/created.txt")); err != nil {
		t.Errorf("file outside the guard was touched: %v", err)
	}
}
