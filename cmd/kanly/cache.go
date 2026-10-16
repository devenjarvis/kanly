package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/devenjarvis/kanly/internal/runner"
)

// A dependent's tests are set up (baseline and per-test coverage) once per
// mutated package and run, which in an edit-and-re-run loop repeats the same
// work while the dependent and the code it links stay unchanged. The setup
// is cached under a key that changes whenever its outcome could: the two
// test binaries (byte-identical when rebuilt from the same source; they
// hold the mutated package's code but not its tests), the flags the tests
// run with, and the size and modification time of every file in the module,
// for the fixtures tests read at run time.

// dependentSetup is the cached outcome of setting up a dependent's tests.
type dependentSetup struct {
	Baseline runner.Baseline `json:"baseline"`
	Coverage []coveredLine   `json:"coverage"`
}

type coveredLine struct {
	File string           `json:"file"`
	Line int              `json:"line"`
	Hits []runner.TestHit `json:"hits"`
}

func (d dependentSetup) covMap() map[runner.FileLine][]runner.TestHit {
	m := make(map[runner.FileLine][]runner.TestHit, len(d.Coverage))
	for _, c := range d.Coverage {
		m[runner.FileLine{File: c.File, Line: c.Line}] = c.Hits
	}
	return m
}

func newDependentSetup(b runner.Baseline, covMap map[runner.FileLine][]runner.TestHit) dependentSetup {
	d := dependentSetup{Baseline: b}
	for k, hits := range covMap {
		d.Coverage = append(d.Coverage, coveredLine{File: k.File, Line: k.Line, Hits: hits})
	}
	return d
}

// setupCache stores dependentSetups as JSON files in dir. A nil cache
// stores nothing.
type setupCache struct {
	dir string
}

// openSetupCache returns the cache in $KANLY_CACHE_DIR or else the user's
// cache directory, or nil when there is neither.
func openSetupCache() *setupCache {
	if dir := os.Getenv("KANLY_CACHE_DIR"); dir != "" {
		return &setupCache{dir: dir}
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil
	}
	return &setupCache{dir: filepath.Join(dir, "kanly", "dependents-v1")}
}

// key hashes what a dependent's setup depends on; ok is false when part of
// it cannot be read.
func (c *setupCache) key(bins []string, parts ...string) (string, bool) {
	if c == nil {
		return "", false
	}
	h := sha256.New()
	for _, bin := range bins {
		f, err := os.Open(bin)
		if err != nil {
			return "", false
		}
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return "", false
		}
		h.Write([]byte{0})
	}
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), true
}

// moduleStamp hashes the path, size and modification time of the files
// under dir, skipping hidden directories such as .git and Go files outside
// testdata (see above).
func moduleStamp(dir string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".go") && !inTestdata(rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00", rel, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// inTestdata reports whether the slash- or separator-delimited relative
// path has a testdata element.
func inTestdata(rel string) bool {
	for _, elem := range strings.Split(filepath.ToSlash(rel), "/") {
		if elem == "testdata" {
			return true
		}
	}
	return false
}

func (c *setupCache) load(key string) (dependentSetup, bool) {
	var d dependentSetup
	if c == nil || key == "" {
		return d, false
	}
	data, err := os.ReadFile(filepath.Join(c.dir, key+".json"))
	if err != nil || json.Unmarshal(data, &d) != nil {
		return dependentSetup{}, false
	}
	return d, true
}

// store saves d; a cache that cannot be written is only slower.
func (c *setupCache) store(key string, d dependentSetup) {
	if c == nil || key == "" {
		return
	}
	data, err := json.Marshal(d)
	if err != nil || os.MkdirAll(c.dir, 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(c.dir, "tmp-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), filepath.Join(c.dir, key+".json")) != nil {
		os.Remove(tmp.Name())
	}
}
