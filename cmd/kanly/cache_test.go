package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/devenjarvis/kanly/internal/runner"
)

func TestModuleStamp(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stamp := func() string {
		t.Helper()
		s, err := moduleStamp(dir)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	write("go.mod", "module m\n")
	write("p/p.go", "package p\n")
	write("p/p_test.go", "package p\n")
	write("p/testdata/fixture.go", "package fixture\n")
	write("p/testdata/input.txt", "in\n")
	write(".git/HEAD", "ref\n")
	base := stamp()

	for _, tt := range []struct {
		file    string
		changes bool
	}{
		{"p/p.go", false},      // compiled code is in the binaries
		{"p/p_test.go", false}, // editing the mutated package's tests
		{".git/HEAD", false},
		{"p/testdata/fixture.go", true}, // read by tests at run time
		{"p/testdata/input.txt", true},
		{"go.mod", true},
	} {
		write(tt.file, "changed content\n")
		got := stamp()
		if (got != base) != tt.changes {
			t.Errorf("editing %s: stamp changed = %v, want %v", tt.file, got != base, tt.changes)
		}
		base = got
	}

	// A new file counts, and so does a touch with the same content.
	write("p/new.json", "{}")
	if got := stamp(); got == base {
		t.Error("adding p/new.json left the stamp unchanged")
	} else {
		base = got
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "go.mod"), later, later); err != nil {
		t.Fatal(err)
	}
	if stamp() == base {
		t.Error("touching go.mod left the stamp unchanged")
	}

	if _, err := moduleStamp(filepath.Join(dir, "missing")); err == nil {
		t.Error("moduleStamp of a missing directory succeeded")
	}
}

func TestInTestdata(t *testing.T) {
	for rel, want := range map[string]bool{
		"testdata/a.go":     true,
		"p/testdata/q/a.go": true,
		"p/a.go":            false,
		"p/testdatax/a.go":  false,
		"a.go":              false,
	} {
		if got := inTestdata(rel); got != want {
			t.Errorf("inTestdata(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestSetupCache(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.WriteFile(bin, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &setupCache{dir: filepath.Join(dir, "cache")}
	key, ok := c.key([]string{bin}, "stamp", "flags")
	if !ok || len(key) != 64 {
		t.Fatalf("key = %q, %v", key, ok)
	}
	if other, _ := c.key([]string{bin}, "stamp", "other flags"); other == key {
		t.Error("key ignores its parts")
	}
	if split, _ := c.key([]string{bin}, "stampflags"); split == key {
		t.Error("key does not separate its parts")
	}
	if _, ok := c.key([]string{filepath.Join(dir, "missing")}); ok {
		t.Error("key of a missing binary succeeded")
	}
	if err := os.WriteFile(bin, []byte("rebuilt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if rebuilt, _ := c.key([]string{bin}, "stamp", "flags"); rebuilt == key {
		t.Error("key ignores the binary's content")
	}

	if _, ok := c.load(key); ok {
		t.Error("load from an empty cache succeeded")
	}
	want := newDependentSetup(
		runner.Baseline{Tests: []string{"TestA", "TestB"}, Durations: map[string]time.Duration{"TestA": time.Second}, Wall: 2 * time.Second},
		map[runner.FileLine][]runner.TestHit{{File: "/m/p.go", Line: 3}: {{Name: "TestA", Hits: 2}}},
	)
	c.store(key, want)
	got, ok := c.load(key)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("load = %+v, %v; want %+v", got, ok, want)
	}
	if m := got.covMap(); !reflect.DeepEqual(m, map[runner.FileLine][]runner.TestHit{{File: "/m/p.go", Line: 3}: {{Name: "TestA", Hits: 2}}}) {
		t.Errorf("covMap = %v", m)
	}

	// A corrupt entry is a miss, not an error.
	if err := os.WriteFile(filepath.Join(c.dir, key+".json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.load(key); ok {
		t.Error("load of a corrupt entry succeeded")
	}

	// A nil cache stores and finds nothing.
	var none *setupCache
	if _, ok := none.key([]string{bin}); ok {
		t.Error("nil cache made a key")
	}
	none.store(key, want)
	if _, ok := none.load(key); ok {
		t.Error("nil cache loaded an entry")
	}
}

func TestOpenSetupCacheHonoursEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KANLY_CACHE_DIR", dir)
	if c := openSetupCache(); c == nil || c.dir != dir {
		t.Errorf("openSetupCache() = %+v, want dir %s", c, dir)
	}
}

func TestSetupCacheKeyAndStoreFailures(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	c := &setupCache{dir: filepath.Join(dir, "cache")}

	// Binaries are delimited, so moving bytes from one to the next
	// changes the key.
	k1, _ := c.key([]string{write("a1", "ab"), write("b1", "c")})
	k2, _ := c.key([]string{write("a2", "a"), write("b2", "bc")})
	if k1 == k2 {
		t.Error(`key(["ab", "c"]) == key(["a", "bc"])`)
	}
	// A binary that cannot be read, such as a directory, gives no key.
	if _, ok := c.key([]string{dir}); ok {
		t.Error("key of a directory succeeded")
	}

	// A failed store leaves no temporary file behind.
	if err := os.MkdirAll(filepath.Join(c.dir, "blocked.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	c.store("blocked", dependentSetup{})
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("cache dir holds %v after a failed store, want only blocked.json", entries)
	}
}

func TestOpenSetupCacheDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KANLY_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", home) // os.UserCacheDir on Linux
	t.Setenv("HOME", home)           // and on macOS, under Library/Caches
	base, err := os.UserCacheDir()
	if err != nil {
		t.Skip(err)
	}
	if c := openSetupCache(); c == nil || c.dir != filepath.Join(base, "kanly", "dependents-v1") {
		t.Errorf("openSetupCache() = %+v, want <user cache>/kanly/dependents-v1", c)
	}
}

func TestGuardDir(t *testing.T) {
	dir := t.TempDir()
	kept := filepath.Join(dir, "kept.txt")
	if err := os.WriteFile(kept, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	restore := guardDir(dir, "example.com/p", &stderr)
	if err := os.WriteFile(kept, []byte("after, longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(dir, "created.txt")
	if err := os.WriteFile(created, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore()

	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("created.txt is still in the directory: %v", err)
	}
	out := stderr.String()
	for _, want := range []string{
		"example.com/p: moved 1 file(s) created during the run to ",
		"warning: example.com/p: tests modified or deleted 1 file(s); check them before committing: ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr lacks %q:\n%s", want, out)
		}
	}

	// Without changes, restoring says nothing.
	stderr.Reset()
	guardDir(dir, "example.com/p", &stderr)()
	if stderr.Len() != 0 {
		t.Errorf("restoring an unchanged directory printed %q", stderr.String())
	}
}
