package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// A killed mutant usually stays killed while neither its code nor the test
// that killed it changes, so in an edit-and-re-run loop most of the mutant
// loop repeats verdicts it already has. A kill is reused when the mutant's
// key and its killing test's source are unchanged and that test still
// covers the mutant. The key holds the Kanly binary, the module stamp, the
// package, the build and test flags, the fingerprint and the source of the
// declaration holding the mutant. A change elsewhere that a test reaches
// through a helper can still turn a reused kill into a survivor; --no-cache
// re-runs everything. Survivors and timeouts are never reused, so a new
// test is always tried against them.

// killEntry is a reused kill: the mutant's key and the source hash of each
// test that killed it.
type killEntry struct {
	Key     string            `json:"key"`
	Killers map[string]string `json:"killers"`
}

// killCache stores each package's kills, by fingerprint, as one JSON file
// in dir. A nil cache stores nothing.
type killCache struct {
	dir  string
	salt string // the Kanly binary's hash; empty when it cannot be read
}

// openKillCache returns the kill cache in $KANLY_CACHE_DIR or else the
// user's cache directory, or nil when there is neither or the running
// binary cannot be hashed: a rebuilt Kanly may judge mutants differently.
func openKillCache() *killCache {
	dir := os.Getenv("KANLY_CACHE_DIR")
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil
		}
		dir = filepath.Join(base, "kanly")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	salt, ok := hashFile(exe)
	if !ok {
		return nil
	}
	return &killCache{dir: filepath.Join(dir, "kills-v1"), salt: salt}
}

func hashFile(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", false
	}
	return hex.EncodeToString(h.Sum(nil)), true
}

func hashStrings(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (c *killCache) path(importPath string) string {
	return filepath.Join(c.dir, hashStrings(importPath)+".json")
}

// load returns the package's stored kills by fingerprint.
func (c *killCache) load(importPath string) map[string]killEntry {
	if c == nil {
		return nil
	}
	data, err := os.ReadFile(c.path(importPath))
	if err != nil {
		return nil
	}
	var m map[string]killEntry
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
}

// update replaces the package's entries for the fingerprints in set: an
// entry with killers is stored, one without is dropped. Other fingerprints
// keep theirs, so a scoped run does not forget the rest of the package. A
// cache that cannot be written is only slower.
func (c *killCache) update(importPath string, set map[string]killEntry) {
	if c == nil || len(set) == 0 {
		return
	}
	m := c.load(importPath)
	if m == nil {
		m = make(map[string]killEntry)
	}
	for fp, e := range set {
		if len(e.Killers) == 0 {
			delete(m, fp)
		} else {
			m[fp] = e
		}
	}
	data, err := json.Marshal(m)
	if err != nil || os.MkdirAll(c.dir, 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(c.dir, "tmp-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), c.path(importPath)) != nil {
		os.Remove(tmp.Name())
	}
}

// mutantKeys returns each mutation's cache key (see above), or "" for one
// whose source cannot be read. parts are the run-wide components.
func (c *killCache) mutantKeys(muts []mutation.Mutation, parts ...string) []string {
	keys := make([]string, len(muts))
	if c == nil {
		return keys
	}
	declsByFile := make(map[string][]string) // file → hash of the declaration on each line (1-based)
	for i, m := range muts {
		lines, ok := declsByFile[m.File]
		if !ok {
			lines = declHashes(m.File)
			declsByFile[m.File] = lines
		}
		if m.Line <= 0 || m.Line >= len(lines) || lines[m.Line] == "" {
			continue
		}
		keys[i] = hashStrings(append([]string{c.salt, m.Fingerprint, lines[m.Line]}, parts...)...)
	}
	return keys
}

// declHashes reads and parses file and returns, indexed by line, the hash of
// the source of the top-level declaration covering that line. A line outside
// every declaration gets the whole file's hash; nil when the file cannot be
// read or parsed.
func declHashes(file string) []string {
	src, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	tf := fset.File(f.Pos())
	whole := hashStrings(string(src))
	lines := make([]string, tf.LineCount()+1)
	for i := range lines {
		lines[i] = whole
	}
	for _, d := range f.Decls {
		start, end := tf.Offset(d.Pos()), tf.Offset(d.End())
		h := hashStrings(string(src[start:end]))
		for l := tf.Line(d.Pos()); l <= tf.Line(d.End()); l++ {
			lines[l] = h
		}
	}
	return lines
}

// testHashes returns the source hash of each top-level test function in
// dir's _test.go files, by name. A name declared in several files (under
// different build tags) hashes all of them.
func testHashes(dir string) map[string]string {
	files, _ := filepath.Glob(filepath.Join(dir, "*_test.go"))
	sort.Strings(files)
	srcs := make(map[string][]string)
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		tf := fset.File(f.Pos())
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil {
				continue
			}
			srcs[fd.Name.Name] = append(srcs[fd.Name.Name], string(src[tf.Offset(fd.Pos()):tf.Offset(fd.End())]))
		}
	}
	out := make(map[string]string, len(srcs))
	for name, s := range srcs {
		out[name] = hashStrings(s...)
	}
	return out
}
