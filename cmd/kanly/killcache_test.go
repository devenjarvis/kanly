package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDeclHashes(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "p.go")
	const src = `package p

// c is a constant.
var c = 1

func A() int {
	return 1
}

func B() int {
	return 2
}
`
	writeFiles(t, dir, map[string]string{"p.go": src})
	before := declHashes(file)
	if len(before) != 13 {
		t.Fatalf("declHashes: %d entries, want 13 (12 lines + unused 0)", len(before))
	}
	if before[6] != before[8] || before[6] == before[10] || before[4] == before[6] || before[3] == before[4] {
		t.Errorf("declHashes = %q: want one hash per declaration and the file's for line 3", before)
	}

	writeFiles(t, dir, map[string]string{"p.go": strings.Replace(src, "return 2", "return 3", 1)})
	after := declHashes(file)
	if after[6] != before[6] || after[4] != before[4] {
		t.Error("editing B changed the hashes of A or c")
	}
	if after[10] == before[10] || after[3] == before[3] {
		t.Error("editing B left the hash of B, or of the file, unchanged")
	}
	// The declaration's last character counts too.
	writeFiles(t, dir, map[string]string{"p.go": strings.Replace(src, "var c = 1", "var c = 2", 1)})
	if declHashes(file)[4] == before[4] {
		t.Error("editing the end of c's declaration left its hash unchanged")
	}

	if declHashes(filepath.Join(dir, "missing.go")) != nil {
		t.Error("declHashes of a missing file is not nil")
	}
	writeFiles(t, dir, map[string]string{"bad.go": "package"})
	if declHashes(filepath.Join(dir, "bad.go")) != nil {
		t.Error("declHashes of a file that does not parse is not nil")
	}
}

func TestTestHashes(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"a_test.go":   "package p\n\nfunc TestA(t *testing.T) { a() }\n\nfunc TestB(t *testing.T) {}\n\nfunc (s) TestM() {}\n",
		"tag_test.go": "//go:build x\n\npackage p\n\nfunc TestA(t *testing.T) { other() }\n",
		"bad_test.go": "package",
		"p.go":        "package p\n\nfunc TestNotATest() {}\n",
	}
	writeFiles(t, dir, files)
	before := testHashes(dir)
	if len(before) != 2 || before["TestA"] == "" || before["TestB"] == "" {
		t.Fatalf("testHashes = %v, want TestA and TestB", before)
	}

	files["tag_test.go"] = strings.Replace(files["tag_test.go"], "other()", "changed()", 1)
	writeFiles(t, dir, files)
	after := testHashes(dir)
	if after["TestA"] == before["TestA"] {
		t.Error("editing TestA under another build tag left its hash unchanged")
	}
	if after["TestB"] != before["TestB"] {
		t.Error("editing TestA changed the hash of TestB")
	}
}

func TestReusedKiller(t *testing.T) {
	hashes := map[string]string{"TestA": "ha", "TestB": "hb", "TestC": "hc"}
	entry := killEntry{Key: "k", Killers: map[string]string{"TestA": "ha", "TestB": "old", "TestC": "hc"}}
	for _, tt := range []struct {
		name     string
		e        killEntry
		key      string
		covering []string
		want     string
	}{
		{"first covering killer", entry, "k", []string{"TestC", "TestA"}, "TestC"},
		{"changed killer skipped", entry, "k", []string{"TestB", "TestA"}, "TestA"},
		{"only changed killers", entry, "k", []string{"TestB"}, ""},
		{"killer no longer covers", entry, "k", []string{"TestD"}, ""},
		{"key changed", entry, "other", []string{"TestA"}, ""},
		{"no key", killEntry{Killers: entry.Killers}, "", []string{"TestA"}, ""},
		{"no entry", killEntry{}, "k", []string{"TestA"}, ""},
		{"killer without source", killEntry{Key: "k", Killers: map[string]string{"TestX": ""}}, "k", []string{"TestX"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := reusedKiller(tt.e, tt.key, hashes, tt.covering); got != tt.want {
				t.Errorf("reusedKiller = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKillUpdates(t *testing.T) {
	res := func(fp string, st mutation.Status, cached bool, killers ...string) mutation.Result {
		return mutation.Result{Mutation: mutation.Mutation{Fingerprint: fp}, Status: st, KillingTests: killers, Cached: cached}
	}
	results := []mutation.Result{
		res("killed", mutation.StatusKilled, false, "TestA", "TestGone"),
		res("unknown", mutation.StatusKilled, false, "TestGone"),
		res("survived", mutation.StatusSurvived, false),
		res("timeout", mutation.StatusTimeout, false),
		res("reused", mutation.StatusKilled, true, "TestA"),
		res("nokey", mutation.StatusKilled, false, "TestA"),
	}
	keys := []string{"k1", "k2", "k3", "k4", "k5", ""}
	got := killUpdates(results, keys, map[string]string{"TestA": "ha"})
	want := map[string]killEntry{
		"killed":   {Key: "k1", Killers: map[string]string{"TestA": "ha"}},
		"unknown":  {Key: "k2"},
		"survived": {Key: "k3"},
		"timeout":  {Key: "k4"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("killUpdates = %+v, want %+v", got, want)
	}
}

func TestKillCacheUpdate(t *testing.T) {
	c := &killCache{dir: filepath.Join(t.TempDir(), "kills")}
	kill := func(test string) killEntry { return killEntry{Key: "k", Killers: map[string]string{test: "h"}} }
	c.update("m/p", map[string]killEntry{"a": kill("TestA"), "b": kill("TestB")})
	c.update("m/q", map[string]killEntry{"a": kill("TestQ")})
	// A later run drops what no longer holds and keeps what it did not see.
	c.update("m/p", map[string]killEntry{"a": {Key: "k"}, "c": kill("TestC")})
	if got, want := c.load("m/p"), map[string]killEntry{"b": kill("TestB"), "c": kill("TestC")}; !reflect.DeepEqual(got, want) {
		t.Errorf("load(m/p) = %+v, want %+v", got, want)
	}
	if got, want := c.load("m/q"), map[string]killEntry{"a": kill("TestQ")}; !reflect.DeepEqual(got, want) {
		t.Errorf("load(m/q) = %+v, want %+v", got, want)
	}

	if err := os.WriteFile(c.path("m/p"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := c.load("m/p"); got != nil {
		t.Errorf("load of a corrupt file = %+v, want nil", got)
	}
	// Nothing to change writes nothing.
	c.update("m/empty", nil)
	// A failed write leaves no temporary file behind.
	if err := os.Remove(c.path("m/q")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(c.path("m/q"), 0o755); err != nil {
		t.Fatal(err)
	}
	c.update("m/q", map[string]killEntry{"a": kill("TestQ")})
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("cache dir holds %d entries after a failed write, want 2", len(entries))
	}

	var none *killCache
	none.update("m/p", map[string]killEntry{"a": kill("TestA")})
	if none.load("m/p") != nil || none.mutantKeys([]mutation.Mutation{{}})[0] != "" {
		t.Error("nil cache stored, loaded or keyed something")
	}
}

func TestMutantKeys(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "p.go")
	const src = "package p\n\nfunc A(a int) int {\n\treturn a + 1\n}\n\nfunc B(b int) int {\n\treturn b - 1\n}\n"
	writeFiles(t, dir, map[string]string{"p.go": src})
	muts := []mutation.Mutation{
		{File: file, Line: 3, Fingerprint: "fa"}, // A's first line
		{File: file, Line: 8, Fingerprint: "fb"},
		{File: file, Line: 10, Fingerprint: "fc"}, // one past the last line
		{File: filepath.Join(dir, "missing.go"), Line: 1, Fingerprint: "fd"},
		{File: file, Line: 0, Fingerprint: "fe"},
		{File: file, Line: 1, Fingerprint: "ff"}, // the package clause
	}
	c := &killCache{salt: "s"}
	before := c.mutantKeys(muts, "stamp")
	if before[0] == "" || before[1] == "" || before[0] == before[1] || before[2] != "" || before[3] != "" || before[4] != "" || before[5] == "" {
		t.Fatalf("mutantKeys = %q: want distinct keys for A, B and line 1, none outside the file's lines or for a missing file", before)
	}
	if got := c.mutantKeys(muts, "other stamp"); got[0] == before[0] {
		t.Error("changing a run-wide part left the key unchanged")
	}
	if got := (&killCache{salt: "t"}).mutantKeys(muts, "stamp"); got[0] == before[0] {
		t.Error("another Kanly binary gave the same key")
	}

	writeFiles(t, dir, map[string]string{"p.go": strings.Replace(src, "return b - 1", "b++\n\treturn b - 1", 1)})
	after := c.mutantKeys(muts, "stamp")
	if after[0] != before[0] || after[1] == before[1] {
		t.Errorf("editing B: key of A changed = %v, of B = %v; want false, true", after[0] != before[0], after[1] != before[1])
	}
}

func TestOpenKillCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KANLY_CACHE_DIR", dir)
	c := openKillCache()
	if c == nil || c.dir != filepath.Join(dir, "kills-v1") || c.salt == "" {
		t.Errorf("openKillCache() = %+v, want <dir>/kills-v1 with the binary's hash", c)
	}

	home := t.TempDir()
	t.Setenv("KANLY_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", home) // os.UserCacheDir on Linux
	t.Setenv("HOME", home)           // and on macOS, under Library/Caches
	base, err := os.UserCacheDir()
	if err != nil {
		t.Skip(err)
	}
	if c := openKillCache(); c == nil || c.dir != filepath.Join(base, "kanly", "kills-v1") {
		t.Errorf("openKillCache() = %+v, want <user cache>/kanly/kills-v1", c)
	}
}

func TestHashStringsDelimitsParts(t *testing.T) {
	if hashStrings("ab", "c") == hashStrings("a", "bc") {
		t.Error(`hashStrings("ab", "c") == hashStrings("a", "bc")`)
	}
}

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a": "x", "b": "y"})
	a, okA := hashFile(filepath.Join(dir, "a"))
	b, okB := hashFile(filepath.Join(dir, "b"))
	if !okA || !okB || a == b {
		t.Errorf("hashFile: %q (%v) and %q (%v), want two distinct hashes", a, okA, b, okB)
	}
	if _, ok := hashFile(filepath.Join(dir, "missing")); ok {
		t.Error("hashFile of a missing file succeeded")
	}
	if _, ok := hashFile(dir); ok {
		t.Error("hashFile of a directory succeeded")
	}
}

// A second run reuses the kills of mutants whose code and killing test are
// unchanged, and re-runs the rest.
func TestRunReusesKills(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Setenv("KANLY_CACHE_DIR", t.TempDir())
	dir := t.TempDir()
	const code = "package kc\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n\nfunc Sub(a, b int) int {\n\treturn a - b\n}\n\nfunc Mul(a, b int) int {\n\treturn a * b\n}\n\nfunc Div(a, b int) int {\n\treturn a / b\n}\n\nfunc Unused(a int) int {\n\treturn a + 1\n}\n"
	const tests = "package kc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tt.Fatal(\"Add\")\n\t}\n}\n\nfunc TestSub(t *testing.T) {\n\tif Sub(5, 3) != 2 {\n\t\tt.Fatal(\"Sub\")\n\t}\n}\n\nfunc TestMul(t *testing.T) { Mul(1, 1); Div(1, 1) }\n"
	writeFiles(t, dir, map[string]string{"go.mod": "module example.com/kc\n\ngo 1.23\n", "kc.go": code, "kc_test.go": tests})
	chdir(t, dir)

	type mutant struct {
		Mutation struct {
			Function    string `json:"function"`
			Fingerprint string `json:"fingerprint"`
		} `json:"mutation"`
		Status       string   `json:"status"`
		KillingTests []string `json:"killing_tests"`
		Cached       bool     `json:"cached"`
	}
	runJSON := func(args ...string) ([]mutant, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := run(append(append([]string{"--format=json"}, args...), "."), &stdout, &stderr); code != 0 {
			t.Fatalf("run %v returned %d; stderr: %s", args, code, stderr.String())
		}
		var r struct{ Mutants []mutant }
		if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
			t.Fatalf("parse JSON: %v\n%s", err, stdout.String())
		}
		return r.Mutants, stderr.String()
	}
	// cachedBy returns, per function, whether each mutant was reused, and
	// checks that verdicts match the first run.
	first, firstErr := runJSON()
	if strings.Contains(firstErr, "kills from earlier runs") {
		t.Errorf("first run reports reused kills:\n%s", firstErr)
	}
	cachedBy := func(ms []mutant) map[string][]bool {
		t.Helper()
		if len(ms) != len(first) {
			t.Fatalf("%d mutants, first run %d", len(ms), len(first))
		}
		out := make(map[string][]bool)
		for i, m := range ms {
			if m.Mutation.Fingerprint != first[i].Mutation.Fingerprint || m.Status != first[i].Status {
				t.Errorf("mutant %s: %s, first run %s %s", m.Mutation.Fingerprint, m.Status, first[i].Mutation.Fingerprint, first[i].Status)
			}
			if m.Cached && (m.Status != "killed" || len(m.KillingTests) != 1) {
				t.Errorf("reused mutant %s: status %s, killers %v", m.Mutation.Fingerprint, m.Status, m.KillingTests)
			}
			out[m.Mutation.Function] = append(out[m.Mutation.Function], m.Cached)
		}
		return out
	}
	all := func(bs []bool, want bool) bool {
		for _, b := range bs {
			if b != want {
				return false
			}
		}
		return len(bs) > 0
	}
	for f, c := range cachedBy(first) {
		if !all(c, false) {
			t.Errorf("first run reused mutants of %s", f)
		}
	}
	for _, args := range [][]string{{"--no-cache"}, {"--kill-matrix"}, {"--verify-kills"}} {
		ms, _ := runJSON(args...)
		for f, c := range cachedBy(ms) {
			if !all(c, false) {
				t.Errorf("%v reused mutants of %s", args, f)
			}
		}
	}

	second, stderr := runJSON()
	got := cachedBy(second)
	if !all(got["Add"], true) || !all(got["Sub"], true) || !all(got["Mul"], false) {
		t.Errorf("second run reused %v; want every Add and Sub mutant, no Mul survivor", got)
	}
	// Progress counts only the mutants left to run.
	notCovered, reused := 0, 0
	for _, m := range second {
		if m.Status == "not_covered" {
			notCovered++
		}
		if m.Cached {
			reused++
		}
	}
	toRun := len(second) - notCovered - reused
	if notCovered == 0 || toRun < 2 {
		t.Fatalf("%d not covered and %d to run; the progress checks need some of each", notCovered, toRun)
	}
	for _, want := range []string{
		fmt.Sprintf("reusing %d kills from earlier runs", reused),
		fmt.Sprintf("testing %d covered mutants (%d not covered) with 3 test(s)", toRun, notCovered),
		fmt.Sprintf(" 1/%d mutants tested", toRun),
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("progress lacks %q:\n%s", want, stderr)
		}
	}
	if last := fmt.Sprintf("%d/%d mutants tested", toRun, toRun); strings.Contains(stderr, last) {
		t.Errorf("progress has %q, which the closing line already covers:\n%s", last, stderr)
	}
	// A run of a single killed mutant reuses just that one.
	for _, m := range second {
		if m.Cached {
			if _, stderr := runJSON("--mutant=" + m.Mutation.Fingerprint); !strings.Contains(stderr, "reusing 1 kills from earlier runs") {
				t.Errorf("--mutant=%s: progress lacks the one reused kill:\n%s", m.Mutation.Fingerprint, stderr)
			}
			break
		}
	}

	// Editing the killing test of Sub, or the code of Add, re-runs them.
	writeFiles(t, dir, map[string]string{
		"kc_test.go": strings.Replace(tests, "Sub(5, 3) != 2", "Sub(7, 3) != 4", 1),
		"kc.go":      strings.Replace(code, "\treturn a + b", "\t// checked\n\treturn a + b", 1),
	})
	third, _ := runJSON()
	got = cachedBy(third)
	if !all(got["Add"], false) || !all(got["Sub"], false) {
		t.Errorf("after editing Add and TestSub, reused %v; want none of Add or Sub", got)
	}
	fourth, _ := runJSON()
	if got = cachedBy(fourth); !all(got["Add"], true) || !all(got["Sub"], true) {
		t.Errorf("run after the edits reused %v; want every Add and Sub mutant", got)
	}
}
