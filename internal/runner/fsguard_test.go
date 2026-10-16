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

	res, err := snap.Restore()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(res.Quarantine)
	removed, quarantine, changed := res.Moved, res.Quarantine, res.Changed
	if len(res.Unmoved) != 0 {
		t.Errorf("unmoved = %v, want none", res.Unmoved)
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
	// Moved, not deleted: contents are recoverable from the quarantine.
	if data, err := os.ReadFile(filepath.Join(quarantine, "testdata/new/deep.txt")); err != nil || string(data) != "created" {
		t.Errorf("quarantined testdata/new/deep.txt: %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(quarantine, "out.txt")); err != nil || string(data) != "created by a test" {
		t.Errorf("quarantined out.txt: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub/created.txt")); err != nil {
		t.Errorf("file outside the guard was touched: %v", err)
	}
}
