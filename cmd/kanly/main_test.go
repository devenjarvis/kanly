package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/report"
	"github.com/devenjarvis/kanly/internal/runner"
	"github.com/devenjarvis/kanly/internal/selector"
)

func relDir(t *testing.T, sub string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	abs, err := filepath.Abs(filepath.Join(filepath.Dir(file), sub))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestRunVersionFlag(t *testing.T) {
	prev := version
	version = "v1.2.3"
	defer func() { version = prev }()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code: want 0, got %d; stderr: %q", code, stderr.String())
	}
	if got := stdout.String(); got != "kanly v1.2.3\n" {
		t.Errorf("stdout: want %q, got %q", "kanly v1.2.3\n", got)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr should be empty, got %q", stderr.String())
	}
}

func TestRunRejectsMissingPackageArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "usage: kanly") {
		t.Errorf("stderr should contain 'usage: kanly', got: %q", stderr.String())
	}
}

func TestRunEndToEndOnSamplePackage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	sampleDir := relDir(t, "../../internal/runner/testdata/sample")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format=json", sampleDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
	}

	var result struct {
		Summary struct {
			Total  int `json:"total"`
			Killed int `json:"killed"`
		} `json:"summary"`
		Packages []struct {
			Package string `json:"package"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON output: %v\n%s", err, stdout.String())
	}

	if result.Summary.Total != 2 {
		t.Errorf("expected Total=2, got %d", result.Summary.Total)
	}
	if result.Summary.Killed != 1 {
		t.Errorf("expected Killed=1, got %d", result.Summary.Killed)
	}
	if len(result.Packages) != 1 {
		t.Errorf("expected 1 package entry, got %d", len(result.Packages))
	}
	const wantPkg = "github.com/devenjarvis/kanly/internal/runner/testdata/sample"
	if len(result.Packages) > 0 && result.Packages[0].Package != wantPkg {
		t.Errorf("Packages[0].Package: want %q, got %q", wantPkg, result.Packages[0].Package)
	}
}

func TestRunFunctionSelector(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	sampleDir := relDir(t, "../../internal/runner/testdata/sample")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format=json", sampleDir + ":Add"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
	}

	var result struct {
		Scope   string `json:"scope"`
		Summary struct {
			Total  int `json:"total"`
			Killed int `json:"killed"`
		} `json:"summary"`
		Mutants []struct {
			Mutation struct {
				Function string `json:"function"`
				Original string `json:"original"`
				Mutant   string `json:"mutant"`
			} `json:"mutation"`
		} `json:"mutants"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout.String())
	}

	// sample has Add (+→-) and Sub (-→+); the selector keeps only Add's mutant.
	if result.Summary.Total != 1 {
		t.Errorf("Total: want 1, got %d", result.Summary.Total)
	}
	if result.Summary.Killed != 1 {
		t.Errorf("Killed: want 1, got %d", result.Summary.Killed)
	}
	for _, m := range result.Mutants {
		if m.Mutation.Function != "Add" {
			t.Errorf("mutant function: want Add, got %q", m.Mutation.Function)
		}
		if m.Mutation.Original != "+" || m.Mutation.Mutant != "-" {
			t.Errorf("mutant op: want +→-, got %s→%s", m.Mutation.Original, m.Mutation.Mutant)
		}
	}
	if !strings.Contains(result.Scope, ":Add") {
		t.Errorf("Scope should mention :Add, got %q", result.Scope)
	}
}

func TestRunFunctionSelectorNoMatchErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	sampleDir := relDir(t, "../../internal/runner/testdata/sample")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format=json", sampleDir + ":Nonexistent"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit, got 0; stdout: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no function matches") {
		t.Errorf("stderr should explain the missing function, got: %s", stderr.String())
	}
	// Suggestions should include real names.
	if !strings.Contains(stderr.String(), "Add") && !strings.Contains(stderr.String(), "Sub") {
		t.Errorf("stderr should suggest known funcs, got: %s", stderr.String())
	}
}

func TestRunMutantIDFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	sampleDir := relDir(t, "../../internal/runner/testdata/sample")

	// Discover the IDs assigned to each function by running without filter first.
	var probeOut, probeErr bytes.Buffer
	if code := run([]string{"--format=json", sampleDir}, &probeOut, &probeErr); code != 0 {
		t.Fatalf("probe run returned %d: %s", code, probeErr.String())
	}
	var probe struct {
		Mutants []struct {
			Mutation struct {
				ID       int    `json:"mutation_id"`
				Function string `json:"function"`
			} `json:"mutation"`
		} `json:"mutants"`
	}
	if err := json.Unmarshal(probeOut.Bytes(), &probe); err != nil {
		t.Fatalf("parse probe JSON: %v", err)
	}
	var subID int
	for _, m := range probe.Mutants {
		if m.Mutation.Function == "Sub" {
			subID = m.Mutation.ID
			break
		}
	}
	if subID == 0 {
		t.Fatalf("could not find Sub mutant ID in probe: %s", probeOut.String())
	}

	// Now narrow with --mutant.
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format=json", "--mutant=" + strconv.Itoa(subID), sampleDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
	}
	var result struct {
		Summary struct {
			Total int `json:"total"`
		} `json:"summary"`
		Mutants []struct {
			Mutation struct {
				ID       int    `json:"mutation_id"`
				Function string `json:"function"`
			} `json:"mutation"`
		} `json:"mutants"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout.String())
	}
	if result.Summary.Total != 1 {
		t.Errorf("Total: want 1, got %d", result.Summary.Total)
	}
	if len(result.Mutants) != 1 || result.Mutants[0].Mutation.ID != subID {
		t.Errorf("expected only mutant id %d, got %+v", subID, result.Mutants)
	}
}

func TestRunMutantFingerprintFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// sample: TestAdd checks Add, TestSub runs Sub without catching its
	// surviving mutant.
	sampleDir := relDir(t, "../../internal/runner/testdata/sample")
	type report struct {
		Mutants []struct {
			Mutation struct {
				Function    string `json:"function"`
				Fingerprint string `json:"fingerprint"`
			} `json:"mutation"`
		} `json:"mutants"`
		Tests []struct {
			Name string `json:"name"`
		} `json:"tests"`
		ZeroKillTests    []string `json:"zero_kill_tests"`
		UnmatchedMutants []string `json:"unmatched_mutants"`
	}
	runOut := func(args ...string) (string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := run(append(append([]string{"--quiet"}, args...), sampleDir), &stdout, &stderr); code != 0 {
			t.Fatalf("run %v returned %d; stderr: %s", args, code, stderr.String())
		}
		return stdout.String(), stderr.String()
	}
	runJSON := func(args ...string) (report, string) {
		t.Helper()
		stdout, stderr := runOut(append([]string{"--format=json"}, args...)...)
		var r report
		if err := json.Unmarshal([]byte(stdout), &r); err != nil {
			t.Fatalf("parse JSON: %v\n%s", err, stdout)
		}
		return r, stderr
	}

	probe, _ := runJSON()
	var subFP string
	for _, m := range probe.Mutants {
		if m.Mutation.Function == "Sub" {
			subFP = m.Mutation.Fingerprint
		}
	}
	if subFP == "" {
		t.Fatalf("no Sub mutant in %+v", probe)
	}

	got, stderr := runJSON("--mutant=" + subFP)
	if len(got.Mutants) != 1 || got.Mutants[0].Mutation.Fingerprint != subFP {
		t.Errorf("--mutant=%s ran %+v, want just that mutant", subFP, got.Mutants)
	}
	// The inventory narrows to the tests that run the selected mutant.
	if len(got.Tests) != 1 || got.Tests[0].Name != "TestSub" {
		t.Errorf("tests = %+v, want just TestSub, the one covering Sub", got.Tests)
	}
	if want := []string{"github.com/devenjarvis/kanly/internal/runner/testdata/sample.TestSub"}; !reflect.DeepEqual(got.ZeroKillTests, want) {
		t.Errorf("zero_kill_tests = %v, want %v", got.ZeroKillTests, want)
	}
	if len(got.UnmatchedMutants) > 0 || strings.Contains(stderr, "no mutant in scope") {
		t.Errorf("every fingerprint matched, yet unmatched_mutants = %v; stderr: %q", got.UnmatchedMutants, stderr)
	}

	const gone1, gone2 = "00000000000a", "000000000001"
	got, stderr = runJSON("--mutant=" + gone1 + "," + subFP + "," + gone2)
	if len(got.Mutants) != 1 {
		t.Errorf("ran %+v, want just the Sub mutant", got.Mutants)
	}
	if want := []string{gone2, gone1}; !reflect.DeepEqual(got.UnmatchedMutants, want) {
		t.Errorf("unmatched_mutants = %v, want %v", got.UnmatchedMutants, want)
	}
	if !strings.Contains(stderr, gone2+", "+gone1) {
		t.Errorf("stderr does not list the unmatched fingerprints in order: %q", stderr)
	}

	// The brief shows the function's source and re-runs by fingerprint.
	brief, _ := runOut("--format=llm", "--mutant="+subFP)
	for _, want := range []string{"**Source: ", "--mutant=" + subFP + " "} {
		if !strings.Contains(brief, want) {
			t.Errorf("llm brief lacks %q:\n%s", want, brief)
		}
	}
}

func TestRunTestsRegexNarrowsInventory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	sampleDir := relDir(t, "../../internal/runner/testdata/sample")
	var stdout, stderr bytes.Buffer
	// Restrict to TestAdd only — Sub mutant should now appear as not_covered.
	code := run([]string{"--format=json", "--tests=^TestAdd$", sampleDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
	}
	var result struct {
		Summary struct {
			Total      int `json:"total"`
			Killed     int `json:"killed"`
			NotCovered int `json:"not_covered"`
		} `json:"summary"`
		Tests []struct {
			Name string `json:"name"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout.String())
	}
	if result.Summary.Total != 2 {
		t.Errorf("Total: want 2, got %d", result.Summary.Total)
	}
	if result.Summary.Killed != 1 {
		t.Errorf("Killed: want 1, got %d", result.Summary.Killed)
	}
	if result.Summary.NotCovered != 1 {
		t.Errorf("NotCovered: want 1 (Sub mutant uncovered when TestSub excluded), got %d", result.Summary.NotCovered)
	}
	names := make([]string, 0, len(result.Tests))
	for _, ts := range result.Tests {
		names = append(names, ts.Name)
	}
	sort.Strings(names)
	want := []string{"TestAdd"}
	if !equalStringsSorted(names, want) {
		t.Errorf("inventory: want %v, got %v", want, names)
	}
}

func equalStringsSorted(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestScopedRunKeepsSingleHitTests guards against narrowing a mutant's
// covering tests by hit count. TestEnable executes Enable once and is the
// only test that asserts on it; TestBulk executes it five times and asserts
// nothing. A scoped run must reach the same verdict as a full run.
func TestScopedRunKeepsSingleHitTests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	sampleDir := relDir(t, "../../internal/runner/testdata/singlehitsample")
	for _, arg := range []string{sampleDir, sampleDir + ":Enable"} {
		t.Run(filepath.Base(arg), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{"--format=json", arg}, &stdout, &stderr); code != 0 {
				t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
			}
			var result struct {
				Mutants []struct {
					Status        string   `json:"status"`
					CoveringTests []string `json:"covering_tests"`
					KillingTests  []string `json:"killing_tests"`
				} `json:"mutants"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatalf("parse JSON: %v\n%s", err, stdout.String())
			}
			if len(result.Mutants) != 1 {
				t.Fatalf("want 1 mutant, got %d", len(result.Mutants))
			}
			m := result.Mutants[0]
			if m.Status != "killed" {
				t.Errorf("status: want killed, got %s (covering %v)", m.Status, m.CoveringTests)
			}
			if !equalStringsSorted(m.CoveringTests, []string{"TestBulk", "TestEnable"}) {
				t.Errorf("covering_tests: want [TestBulk TestEnable], got %v", m.CoveringTests)
			}
			if !equalStringsSorted(m.KillingTests, []string{"TestEnable"}) {
				t.Errorf("killing_tests: want [TestEnable], got %v", m.KillingTests)
			}
		})
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestRunMultiplePositionalArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	sampleDir := relDir(t, "../../internal/runner/testdata/sample")
	cmpDir := relDir(t, "../../internal/runner/testdata/cmpsample")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format=json", sampleDir, cmpDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
	}

	var result struct {
		Summary struct {
			Total int `json:"total"`
		} `json:"summary"`
		Packages []struct {
			Package string `json:"package"`
		} `json:"packages"`
		Mutants []struct {
			Mutation struct {
				Package string `json:"package"`
			} `json:"mutation"`
		} `json:"mutants"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON output: %v\n%s", err, stdout.String())
	}

	// sample: 2 int_arith mutants (Add +→-, Sub -→+).
	// cmpsample: 2 cmp mutants (>→>=, >→<=) + 2 int_literal mutants on `0` (→1, →-1).
	if result.Summary.Total != 6 {
		t.Errorf("expected Total=6, got %d", result.Summary.Total)
	}
	if len(result.Packages) != 2 {
		t.Errorf("expected 2 package entries, got %d", len(result.Packages))
	}
	for i, m := range result.Mutants {
		if m.Mutation.Package == "" {
			t.Errorf("Mutants[%d].Mutation.Package is empty", i)
		}
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		{"a", "b", 1},
		{"ab", "a", 1},
		{"a", "ab", 1},
		{"kitten", "sitting", 3},
		{"abc", "xyz", 3},
		{"ab", "ba", 2},
		{"abc", "abd", 1},
	}
	for _, tt := range tests {
		t.Run(tt.a+"/"+tt.b, func(t *testing.T) {
			if got := levenshtein(tt.a, tt.b); got != tt.want {
				t.Errorf("levenshtein(%q, %q): want %d, got %d", tt.a, tt.b, tt.want, got)
			}
		})
	}
}

func TestMin3(t *testing.T) {
	tests := []struct {
		name    string
		a, b, c int
		want    int
	}{
		{"a smallest", 1, 2, 3, 1},
		{"b smallest", 3, 1, 2, 1},
		{"c smallest", 3, 2, 1, 1},
		{"all equal", 5, 5, 5, 5},
		{"a and b tie", 2, 2, 3, 2},
		{"a and c tie", 2, 3, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := min3(tt.a, tt.b, tt.c); got != tt.want {
				t.Errorf("min3(%d,%d,%d): want %d, got %d", tt.a, tt.b, tt.c, tt.want, got)
			}
		})
	}
}

func TestParseMutants(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		want            *mutantSet
		wantErrContains string
	}{
		{name: "empty", input: "", want: nil},
		{name: "whitespace only", input: "  ", want: nil},
		{name: "single id", input: "1", want: &mutantSet{ids: map[int]bool{1: true}, fps: map[string]bool{}}},
		{name: "multiple ids", input: "1,2,3", want: &mutantSet{ids: map[int]bool{1: true, 2: true, 3: true}, fps: map[string]bool{}}},
		{name: "spaces around ids", input: " 2 , 3 ", want: &mutantSet{ids: map[int]bool{2: true, 3: true}, fps: map[string]bool{}}},
		{name: "fingerprints and ids", input: "5e0d8f5590d5, 7,000000000012", want: &mutantSet{ids: map[int]bool{7: true}, fps: map[string]bool{"5e0d8f5590d5": true, "000000000012": true}}},
		{name: "zero is invalid", input: "0", wantErrContains: "must be >= 1"},
		{name: "negative is invalid", input: "-1", wantErrContains: "must be >= 1"},
		{name: "non-integer", input: "abc", wantErrContains: "invalid id"},
		{name: "short hex is not a fingerprint", input: "5e0d8f", wantErrContains: "12-hex-digit fingerprint"},
		{name: "upper-case hex is not a fingerprint", input: "5E0D8F5590D5", wantErrContains: "invalid id"},
		{name: "empty part from comma", input: ",", wantErrContains: "empty id"},
		{name: "trailing comma", input: "1,", wantErrContains: "empty id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMutants(tt.input)
			if tt.wantErrContains != "" {
				if err == nil {
					t.Fatalf("parseMutants(%q): expected error containing %q, got nil", tt.input, tt.wantErrContains)
				}
				if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("parseMutants(%q): error %q does not contain %q", tt.input, err.Error(), tt.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMutants(%q): unexpected error: %v", tt.input, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseMutants(%q): want %+v, got %+v", tt.input, tt.want, got)
			}
		})
	}
}

func TestMutantSetHas(t *testing.T) {
	set := &mutantSet{ids: map[int]bool{2: true}, fps: map[string]bool{"5e0d8f5590d5": true}}
	for _, tt := range []struct {
		m    mutation.Mutation
		want bool
	}{
		{mutation.Mutation{ID: 2, Fingerprint: "aaaaaaaaaaaa"}, true},
		{mutation.Mutation{ID: 3, Fingerprint: "5e0d8f5590d5"}, true},
		{mutation.Mutation{ID: 3, Fingerprint: "aaaaaaaaaaaa"}, false},
		{mutation.Mutation{ID: 3}, false},
	} {
		if got := set.has(tt.m); got != tt.want {
			t.Errorf("has(%+v) = %v, want %v", tt.m, got, tt.want)
		}
	}
}

func TestDescribeScope(t *testing.T) {
	tests := []struct {
		name     string
		specs    []selector.Spec
		diff     bool
		diffBase string
		tests    string
		mutants  string
		want     string
	}{
		{
			name: "no filters",
			want: "",
		},
		{
			name:  "single spec no funcs",
			specs: []selector.Spec{{Pattern: "./internal/foo"}},
			want:  "./internal/foo",
		},
		{
			name:  "single spec with one func",
			specs: []selector.Spec{{Pattern: "./internal/foo", Funcs: []string{"Foo"}}},
			want:  "./internal/foo:Foo",
		},
		{
			name:  "single spec with multiple funcs",
			specs: []selector.Spec{{Pattern: "./internal/foo", Funcs: []string{"Foo", "Bar"}}},
			want:  "./internal/foo:Foo,Bar",
		},
		{
			name:     "diff flag",
			diff:     true,
			diffBase: "HEAD",
			want:     "--diff=HEAD",
		},
		{
			name:  "tests flag",
			tests: "^TestFoo$",
			want:  "--tests=^TestFoo$",
		},
		{
			name:    "mutants flag",
			mutants: "1,2,3",
			want:    "--mutant=1,2,3",
		},
		{
			name:  "spec and tests together",
			specs: []selector.Spec{{Pattern: "./..."}},
			tests: "^TestFoo$",
			want:  "./... --tests=^TestFoo$",
		},
		{
			name:     "diff with custom base",
			diff:     true,
			diffBase: "origin/main",
			want:     "--diff=origin/main",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describeScope(tt.specs, tt.diff, tt.diffBase, tt.tests, tt.mutants)
			if got != tt.want {
				t.Errorf("describeScope: want %q, got %q", tt.want, got)
			}
		})
	}
}

// TestRunKillsPackageInitMutants guards mutants that execute during package
// initialization: in package-level var initializers (outside every coverage
// block) and in init() functions (which can run before the dispatcher's own
// init). Both are asserted on precisely, so every mutant must be killed.
func TestRunKillsPackageInitMutants(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := relDir(t, "../../internal/runner/testdata/initsample")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--format=json", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
	}
	var result struct {
		Mutants []struct {
			Mutation struct {
				Line     int    `json:"line"`
				Function string `json:"function"`
				Operator string `json:"operator"`
				Mutant   string `json:"mutant"`
			} `json:"mutation"`
			Status string `json:"status"`
		} `json:"mutants"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON output: %v\n%s", err, stdout.String())
	}
	if len(result.Mutants) == 0 {
		t.Fatal("no mutants generated")
	}
	for _, m := range result.Mutants {
		// base = 3 is only read through Total and Inited; every value
		// change is observable.
		if m.Status != "killed" {
			t.Errorf("line %d (%q) %s→%s: status %s, want killed",
				m.Mutation.Line, m.Mutation.Function, m.Mutation.Operator, m.Mutation.Mutant, m.Status)
		}
	}
}

// TestRunCoversCaseExpressions guards against case expressions counting as
// not covered: TestKind takes no case, but evaluates them all, so it must run
// against, and kill, each negated comparison.
func TestRunCoversCaseExpressions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := relDir(t, "../../internal/runner/testdata/casesample")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--format=json", "--operators=int_cmp_negate", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
	}
	var result struct {
		Mutants []struct {
			Mutation struct {
				Line     int    `json:"line"`
				Original string `json:"original"`
			} `json:"mutation"`
			Status string `json:"status"`
		} `json:"mutants"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON output: %v\n%s", err, stdout.String())
	}
	got := map[int]string{}
	for _, m := range result.Mutants {
		got[m.Mutation.Line] = m.Status
	}
	want := map[int]string{8: "killed", 10: "killed", 11: "killed"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("status by line: want %v, got %v", want, got)
	}
}

// TestRunReportsHealthyPackagesWhenOthersFail guards against one bad package
// discarding the whole run: the healthy package's results must be reported,
// each failure listed with the stage it failed in, and the exit code must
// still signal that the run is incomplete.
func TestRunReportsHealthyPackagesWhenOthersFail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := relDir(t, "../../internal/runner/testdata/partial")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format=json", dir + "/..."}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code: want 1 (incomplete run), got %d; stderr: %s", code, stderr.String())
	}

	var result struct {
		Packages []struct {
			Package string `json:"package"`
			Killed  int    `json:"killed"`
		} `json:"packages"`
		Errors []struct {
			Package string `json:"package"`
			Stage   string `json:"stage"`
			Error   string `json:"error"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON output: %v\n%s", err, stdout.String())
	}

	const base = "github.com/devenjarvis/kanly/internal/runner/testdata/partial/"
	if len(result.Packages) != 1 || result.Packages[0].Package != base+"good" || result.Packages[0].Killed != 1 {
		t.Errorf("packages: want only %sgood with 1 kill, got %+v", base, result.Packages)
	}
	stages := make(map[string]string)
	for _, e := range result.Errors {
		stages[e.Package] = e.Stage
		if e.Error == "" {
			t.Errorf("%s: empty error message", e.Package)
		}
	}
	want := map[string]string{base + "broken": "baseline", base + "typeerr": "load"}
	for pkg, stage := range want {
		if stages[pkg] != stage {
			t.Errorf("errors[%s].stage: want %q, got %q (all errors: %+v)", pkg, stage, stages[pkg], result.Errors)
		}
	}
	if len(result.Errors) != len(want) {
		t.Errorf("errors: want %d entries, got %+v", len(want), result.Errors)
	}
}

// TestRunDerivesMutantTimeoutFromBaseline guards against every hanging mutant
// costing a fixed worst-case timeout: TestCount takes milliseconds, so the
// infinite-loop mutant must be cut off after a few seconds, not 30.
func TestRunDerivesMutantTimeoutFromBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := relDir(t, "../../internal/runner/testdata/loopsample")
	var stdout, stderr bytes.Buffer
	start := time.Now()
	if code := run([]string{"--format=json", dir + ":Count"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("run took %v; the looping mutant should time out within a few seconds", elapsed)
	}
	var result struct {
		Mutants []struct {
			Mutation struct {
				Operator string `json:"operator"`
				Original string `json:"original"`
			} `json:"mutation"`
			Status string `json:"status"`
		} `json:"mutants"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON output: %v\n%s", err, stdout.String())
	}
	timeouts := 0
	for _, m := range result.Mutants {
		if m.Status == "timeout" {
			timeouts++
			if m.Mutation.Operator != "inc_dec" {
				t.Errorf("unexpected timeout for %s %s", m.Mutation.Operator, m.Mutation.Original)
			}
		}
	}
	if timeouts != 1 {
		t.Errorf("want exactly the i++→i-- mutant to time out, got %d timeouts: %+v", timeouts, result.Mutants)
	}
}

func TestRunRejectsInvalidTimeoutFlags(t *testing.T) {
	for _, args := range [][]string{{"--timeout=-1s", "."}, {"--timeout-factor=0.5", "."}, {"--llm-limit=-1", "."}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("run(%v) = %d, want 2; stderr: %s", args, code, stderr.String())
		}
		if flagName, _, _ := strings.Cut(args[0], "="); !strings.Contains(stderr.String(), flagName) {
			t.Errorf("run(%v): stderr does not name %s: %s", args, flagName, stderr.String())
		}
	}
	// --llm-limit=0 lists everything and is valid: the run fails on the
	// next flag instead.
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--llm-limit=0", "--jobs=0", "."}, &stdout, &stderr); code != 2 || strings.Contains(stderr.String(), "--llm-limit") {
		t.Errorf("--llm-limit=0: exit %d, stderr %q; want only --jobs rejected", code, stderr.String())
	}
}

// Tests that fail without a mutant are excluded with a reason that says why,
// and the package is still mutation-tested with the tests that pass.
func TestRunExcludesFailingTests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := relDir(t, "../../internal/runner/testdata/failingsample")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--format=json", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
	}
	var result struct {
		Excluded []struct {
			Test   string `json:"test"`
			Reason string `json:"reason"`
		} `json:"excluded_tests"`
		Mutants []struct {
			Mutation struct {
				Function string `json:"function"`
			} `json:"mutation"`
			Status       string   `json:"status"`
			KillingTests []string `json:"killing_tests"`
		} `json:"mutants"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout.String())
	}
	wantReason := map[string]string{
		"TestBroken":     "fails on the unmodified code",
		"TestCallerName": "fails only under Kanly's rewritten code",
		"TestSecond":     "fails when run on its own",
	}
	got := make(map[string]string)
	for _, e := range result.Excluded {
		got[e.Test] = e.Reason
	}
	for name, want := range wantReason {
		if !strings.HasPrefix(got[name], want) {
			t.Errorf("%s: reason %q, want prefix %q", name, got[name], want)
		}
	}
	if len(got) != len(wantReason) {
		t.Errorf("excluded tests = %v, want exactly %v", got, wantReason)
	}
	if !strings.Contains(got["TestSecond"], "TestFirst did not run first") {
		t.Errorf("TestSecond reason should carry the failure message, got %q", got["TestSecond"])
	}
	for _, m := range result.Mutants {
		if m.Mutation.Function == "Add" && m.Status != "killed" {
			t.Errorf("Add mutant: status %s, want killed by TestAdd", m.Status)
		}
		for _, k := range m.KillingTests {
			if _, excluded := wantReason[k]; excluded {
				t.Errorf("excluded test %s was run against a mutant", k)
			}
		}
	}
}

// A failure outside any test (here TestMain) cannot be narrowed to tests to
// exclude, so the package is reported as not tested.
func TestRunReportsTestMainFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := relDir(t, "../../internal/runner/testdata/testmainfail")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--format=json", dir}, &stdout, &stderr); code != 1 {
		t.Fatalf("run returned %d, want 1; stderr: %s", code, stderr.String())
	}
	var result struct {
		Errors []struct {
			Stage string `json:"stage"`
			Error string `json:"error"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout.String())
	}
	if len(result.Errors) != 1 || result.Errors[0].Stage != "baseline" {
		t.Fatalf("errors = %+v, want one baseline error", result.Errors)
	}
	for _, want := range []string{"fail with no mutant applied", "database unavailable"} {
		if !strings.Contains(result.Errors[0].Error, want) {
			t.Errorf("error %q does not mention %q", result.Errors[0].Error, want)
		}
	}

	// A TestMain that fails only under the schema is reported as a Kanly bug.
	stdout.Reset()
	stderr.Reset()
	dir = relDir(t, "../../internal/runner/testdata/testmainschema")
	if code := run([]string{"--format=json", dir}, &stdout, &stderr); code != 1 {
		t.Fatalf("run returned %d, want 1; stderr: %s", code, stderr.String())
	}
	result.Errors = nil
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout.String())
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0].Error, "pass on the unmodified code but fail under Kanly's rewritten code") {
		t.Errorf("errors = %+v, want a baseline error blaming the rewrite", result.Errors)
	}
}

func TestConfirmVerdicts(t *testing.T) {
	mut := func(id int, status mutation.Status) mutation.Result {
		return mutation.Result{Mutation: mutation.Mutation{ID: id}, Status: status, CoveringTests: []string{"TestX"}}
	}
	// rerun returns the verdict queued for the mutant and records the scale.
	type call struct {
		id    int
		scale float64
	}
	fake := func(next map[int]mutation.Status, calls *[]call) func(int, mutation.Result, float64) (mutation.Result, error) {
		return func(i int, r mutation.Result, scale float64) (mutation.Result, error) {
			if i+1 != r.Mutation.ID {
				t.Errorf("re-run of mutant %d got index %d", r.Mutation.ID, i)
			}
			*calls = append(*calls, call{r.Mutation.ID, scale})
			r.Status = next[r.Mutation.ID]
			return r, nil
		}
	}

	t.Run("timeouts are re-run with a longer budget", func(t *testing.T) {
		results := []mutation.Result{mut(1, mutation.StatusTimeout), mut(2, mutation.StatusTimeout), mut(3, mutation.StatusKilled)}
		var calls []call
		next := map[int]mutation.Status{1: mutation.StatusKilled, 2: mutation.StatusTimeout}
		if err := confirmVerdicts(results, false, fake(next, &calls)); err != nil {
			t.Fatal(err)
		}
		if want := []call{{1, confirmTimeoutScale}, {2, confirmTimeoutScale}}; !reflect.DeepEqual(calls, want) {
			t.Errorf("re-runs = %v, want %v (kills are not re-run without verifyKills)", calls, want)
		}
		if results[0].Status != mutation.StatusKilled || results[0].Note == "" {
			t.Errorf("mutant 1: got %s %q, want killed with a note", results[0].Status, results[0].Note)
		}
		if results[1].Status != mutation.StatusTimeout || results[1].Note != "" {
			t.Errorf("mutant 2: got %s %q, want a confirmed timeout without a note", results[1].Status, results[1].Note)
		}
	})

	t.Run("verifyKills demotes kills that do not reproduce", func(t *testing.T) {
		results := []mutation.Result{mut(1, mutation.StatusKilled), mut(2, mutation.StatusKilled), mut(3, mutation.StatusSurvived)}
		var calls []call
		next := map[int]mutation.Status{1: mutation.StatusSurvived, 2: mutation.StatusKilled}
		if err := confirmVerdicts(results, true, fake(next, &calls)); err != nil {
			t.Fatal(err)
		}
		if len(calls) != 2 {
			t.Errorf("re-runs = %v, want the two kills only", calls)
		}
		if results[0].Status != mutation.StatusSurvived || !strings.Contains(results[0].Note, "not reproducible") {
			t.Errorf("mutant 1: got %s %q, want survived with a note", results[0].Status, results[0].Note)
		}
		if results[1].Status != mutation.StatusKilled {
			t.Errorf("mutant 2: got %s, want killed", results[1].Status)
		}
	})
}

// --verify-kills re-runs a kill alone and reports it as survived when it
// does not reproduce.
func TestRunVerifyKills(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := relDir(t, "../../internal/runner/testdata/flakykill")
	statuses := func(args ...string) []string {
		t.Helper()
		t.Setenv("FLAKY_MARKER", filepath.Join(t.TempDir(), "marker"))
		var stdout, stderr bytes.Buffer
		if code := run(append([]string{"--format=json", "--jobs=1", "--mutant=1"}, args...), &stdout, &stderr); code != 0 {
			t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
		}
		var result struct {
			Mutants []struct {
				Status string `json:"status"`
			} `json:"mutants"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("parse JSON: %v\n%s", err, stdout.String())
		}
		var out []string
		for _, m := range result.Mutants {
			out = append(out, m.Status)
		}
		return out
	}
	if got := statuses(dir); !reflect.DeepEqual(got, []string{"killed"}) {
		t.Errorf("without --verify-kills: statuses %v, want [killed]", got)
	}
	if got := statuses("--verify-kills", dir); !reflect.DeepEqual(got, []string{"survived"}) {
		t.Errorf("with --verify-kills: statuses %v, want [survived]", got)
	}
}

// A .kanlyignore entry hides its mutant, and an entry matching no mutant is
// reported as stale. --fail-fast keeps every verdict but stops each mutant
// at its first failing test, so the per-test views are dropped.
func TestRunAcceptedMutantsAndFailFast(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := relDir(t, "../../internal/runner/testdata/acceptsample")
	type result struct {
		FailFast bool `json:"fail_fast"`
		Summary  struct {
			Ignored int `json:"ignored"`
		} `json:"summary"`
		Tests        []json.RawMessage `json:"tests"`
		StaleIgnores []struct {
			Fingerprint string `json:"fingerprint"`
		} `json:"stale_ignores"`
		Mutants []struct {
			Mutation struct {
				ID          int    `json:"mutation_id"`
				Fingerprint string `json:"fingerprint"`
			} `json:"mutation"`
			Status       string   `json:"status"`
			KillingTests []string `json:"killing_tests"`
		} `json:"mutants"`
	}
	runJSON := func(args ...string) result {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := run(append(append([]string{"--format=json"}, args...), dir), &stdout, &stderr); code != 0 {
			t.Fatalf("run %v returned %d; stderr: %s", args, code, stderr.String())
		}
		var r result
		if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
			t.Fatalf("parse JSON: %v\n%s", err, stdout.String())
		}
		return r
	}

	full := runJSON()
	if full.Summary.Ignored != 1 {
		t.Errorf("ignored = %d, want 1", full.Summary.Ignored)
	}
	for _, m := range full.Mutants {
		if m.Mutation.Fingerprint == "f5599e05f98a" {
			t.Errorf("accepted mutant %d was run", m.Mutation.ID)
		}
	}
	if len(full.StaleIgnores) != 1 || full.StaleIgnores[0].Fingerprint != "0123456789ab" {
		t.Errorf("stale_ignores = %+v, want just 0123456789ab", full.StaleIgnores)
	}
	if full.FailFast || len(full.Tests) == 0 {
		t.Errorf("full run: fail_fast = %v with %d tests, want false with tests", full.FailFast, len(full.Tests))
	}

	fast := runJSON("--fail-fast")
	if !fast.FailFast || len(fast.Tests) != 0 {
		t.Errorf("--fail-fast: fail_fast = %v with %d tests, want true with none", fast.FailFast, len(fast.Tests))
	}
	if len(fast.Mutants) != len(full.Mutants) {
		t.Fatalf("--fail-fast ran %d mutants, full run %d", len(fast.Mutants), len(full.Mutants))
	}
	multi := false
	for i, m := range fast.Mutants {
		want := full.Mutants[i]
		if m.Status != want.Status {
			t.Errorf("mutant %d: --fail-fast status %s, full run %s", m.Mutation.ID, m.Status, want.Status)
		}
		multi = multi || len(want.KillingTests) > 1
		if m.Status == "killed" && len(m.KillingTests) != 1 {
			t.Errorf("mutant %d: --fail-fast killing tests %v, want exactly one", m.Mutation.ID, m.KillingTests)
		}
	}
	if !multi {
		t.Error("no mutant has several killers in the full run, so --fail-fast is not exercised")
	}
}

// Files that mutants make tests create in the package directory are moved
// out of it, and each test process gets a private temporary directory.
func TestRunCleansUpFilesTestsCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := relDir(t, "../../internal/runner/testdata/fswrite")
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	var stdout, stderr bytes.Buffer
	if code := run([]string{dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "fswrite-result.txt")); !os.IsNotExist(err) {
		t.Errorf("file written into the package directory by a mutant was not moved out (stat: %v)", err)
	}
	if !strings.Contains(stderr.String(), "moved 1 file(s) created during the run to") {
		t.Errorf("stderr does not report the move:\n%s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tmp, "fswrite-leftover")); !os.IsNotExist(err) {
		t.Errorf("a test's leftover temporary file reached the real TMPDIR (stat: %v)", err)
	}
}

func TestSelectOperators(t *testing.T) {
	ops := mutation.Operators()
	tests := []struct {
		name, only, skip string
		want             []string // nil means all operators
		wantErr          string
	}{
		{name: "none", want: nil},
		{name: "only", only: "int_arith, bool_logic", want: []string{"bool_logic", "int_arith"}},
		{name: "only minus skip", only: "int_arith,bool_logic", skip: "bool_logic", want: []string{"int_arith"}},
		{name: "unknown", only: "int_arth", wantErr: `--operators: unknown operator "int_arth"`},
		{name: "unknown skip", skip: "nope", wantErr: `--skip-operators: unknown operator "nope"`},
		{name: "nothing left", only: "int_arith", skip: "int_arith", wantErr: "leave no operator"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectOperators(ops, tt.only, tt.skip)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == nil {
				if got != nil {
					t.Errorf("got %v, want nil (all operators)", got)
				}
				return
			}
			var names []string
			for n := range got {
				names = append(names, n)
			}
			sort.Strings(names)
			if !reflect.DeepEqual(names, tt.want) {
				t.Errorf("got %v, want %v", names, tt.want)
			}
		})
	}

	// Skipping alone keeps every other operator.
	got, err := selectOperators(ops, "", "call_delete")
	if err != nil {
		t.Fatal(err)
	}
	if got["call_delete"] || len(got) != len(ops)-1 {
		t.Errorf("skip-only: got %d operators (call_delete=%v), want %d", len(got), got["call_delete"], len(ops)-1)
	}
}

func TestExcludeFilter(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	f, err := excludeFilter("internal/gen, *_mock.go, cmd/*/legacy.go")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		file string
		want bool
	}{
		{"internal/gen/x.go", true},
		{"internal/gen/sub/y.go", true},
		{"internal/generated/x.go", false},
		{"pkg/store_mock.go", true},
		{"cmd/kanly/legacy.go", true},
		{"cmd/kanly/main.go", false},
	}
	for _, tt := range tests {
		if got := f(filepath.Join(cwd, tt.file)); got != tt.want {
			t.Errorf("excluded(%s) = %v, want %v", tt.file, got, tt.want)
		}
	}
	if f, err := excludeFilter(" , "); err != nil || f != nil {
		t.Errorf("empty list: got (%v, %v), want (nil, nil)", f != nil, err)
	}
	if _, err := excludeFilter("[bad"); err == nil {
		t.Error("malformed pattern: want an error")
	}
}

// Ignore directives, --operators and --exclude reach the report.
func TestRunSuppression(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := relDir(t, "../../internal/runner/testdata/ignoresample")
	type summary struct {
		Total   int `json:"total"`
		Ignored int `json:"ignored"`
	}
	runJSON := func(args ...string) (summary, []string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := run(append(append([]string{"--format=json"}, args...), dir), &stdout, &stderr); code != 0 {
			t.Fatalf("run %v returned %d; stderr: %s", args, code, stderr.String())
		}
		var result struct {
			Summary summary `json:"summary"`
			Mutants []struct {
				Mutation struct {
					Function string `json:"function"`
					Operator string `json:"operator"`
				} `json:"mutation"`
			} `json:"mutants"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("parse JSON: %v\n%s", err, stdout.String())
		}
		var muts []string
		for _, m := range result.Mutants {
			muts = append(muts, m.Mutation.Function+" "+m.Mutation.Operator)
		}
		return result.Summary, muts
	}

	all, muts := runJSON()
	if all.Ignored != 1 {
		t.Errorf("ignored = %d, want 1 (Scale's int_arith)", all.Ignored)
	}
	for _, m := range muts {
		if m == "Scale int_arith" {
			t.Error("the ignored Scale int_arith mutant ran")
		}
	}
	if _, muts := runJSON("--operators=int_arith"); !reflect.DeepEqual(muts, []string{"Add int_arith"}) {
		t.Errorf("--operators=int_arith ran %v, want [Add int_arith]", muts)
	}
	if s, muts := runJSON("--skip-operators=int_arith"); s.Total != all.Total-1 || containsString(muts, "Add int_arith") {
		t.Errorf("--skip-operators=int_arith ran %v (total %d of %d)", muts, s.Total, all.Total)
	}
	if s, _ := runJSON("--exclude=ignore.go"); s.Total != 0 {
		t.Errorf("--exclude=ignore.go: total %d, want 0", s.Total)
	}
}

// --tags reaches the loader and both compiles; --short reaches every run.
func TestRunTagsAndShort(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := relDir(t, "../../internal/runner/testdata/tagsample")
	statusByFunc := func(args ...string) map[string][]string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := run(append(append([]string{"--format=json", "--operators=int_arith"}, args...), dir), &stdout, &stderr); code != 0 {
			t.Fatalf("run %v returned %d; stderr: %s", args, code, stderr.String())
		}
		var result struct {
			Mutants []struct {
				Mutation struct {
					Function string `json:"function"`
				} `json:"mutation"`
				Status string `json:"status"`
			} `json:"mutants"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("parse JSON: %v\n%s", err, stdout.String())
		}
		out := make(map[string][]string)
		for _, m := range result.Mutants {
			out[m.Mutation.Function] = append(out[m.Mutation.Function], m.Status)
		}
		return out
	}

	plain := statusByFunc()
	if _, ok := plain["Sub"]; ok {
		t.Errorf("without --tags, Sub (integration-tagged) was mutated: %v", plain)
	}
	if got := plain["Double"]; !reflect.DeepEqual(got, []string{"killed"}) {
		t.Errorf("without --short, Double: %v, want [killed]", got)
	}
	tagged := statusByFunc("--tags=integration")
	if got := tagged["Sub"]; !reflect.DeepEqual(got, []string{"killed"}) {
		t.Errorf("with --tags=integration, Sub: %v, want [killed]", got)
	}
	short := statusByFunc("--short")
	if got := short["Double"]; !reflect.DeepEqual(got, []string{"not_covered"}) {
		t.Errorf("with --short, Double: %v, want [not_covered] (its only test skips)", got)
	}
}

func TestRunRejectsUnknownFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--format=xml", "."}, &stdout, &stderr); code != 2 {
		t.Errorf("exit code %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--format: must be text, json, llm or sarif") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

// --min-score gates on the score including not-covered mutants; --quiet
// silences progress.
func TestRunMinScoreAndQuiet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := relDir(t, "../../internal/runner/testdata/sample") // 1 of 2 mutants killed: 50%
	tests := []struct {
		args     []string
		wantCode int
	}{
		{[]string{"--min-score=50"}, 0},
		{[]string{"--min-score=60"}, 3},
	}
	for _, tt := range tests {
		var stdout, stderr bytes.Buffer
		if code := run(append(tt.args, dir), &stdout, &stderr); code != tt.wantCode {
			t.Errorf("%v: exit code %d, want %d; stderr: %s", tt.args, code, tt.wantCode, stderr.String())
		}
		if tt.wantCode == 3 && !strings.Contains(stderr.String(), "is 50.0%, below --min-score=60") {
			t.Errorf("%v: stderr does not explain the failure: %s", tt.args, stderr.String())
		}
		if !strings.Contains(stderr.String(), "kanly: github.com/devenjarvis/kanly/internal/runner/testdata/sample: done in") {
			t.Errorf("%v: no progress on stderr: %s", tt.args, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--quiet", dir}, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Errorf("--quiet: exit %d, stderr %q; want 0 and empty", code, stderr.String())
	}
	if code := run([]string{"--min-score=101", dir}, &stdout, &stderr); code != 2 {
		t.Errorf("--min-score=101: exit %d, want 2", code)
	}
}

func TestRunSARIF(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := relDir(t, "../../internal/runner/testdata/sample")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--format=sarif", "--quiet", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d; stderr: %s", code, stderr.String())
	}
	var log struct {
		Version string `json:"version"`
		Runs    []struct {
			Results []struct {
				RuleID string `json:"ruleId"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &log); err != nil {
		t.Fatalf("parse SARIF: %v\n%s", err, stdout.String())
	}
	if log.Version != "2.1.0" || len(log.Runs) != 1 || len(log.Runs[0].Results) != 1 || log.Runs[0].Results[0].RuleID != "int_arith" {
		t.Errorf("SARIF = %s", stdout.String())
	}
}

// A --tests regex that matches no test leaves every mutant not covered; it
// is not a package failure.
func TestRunTestsRegexMatchingNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := relDir(t, "../../internal/runner/testdata/sample")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--format=json", "--quiet", "--tests=^TestNoSuchTest$", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d; stderr: %s", code, stderr.String())
	}
	var result struct {
		Summary struct {
			Total      int `json:"total"`
			NotCovered int `json:"not_covered"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout.String())
	}
	if result.Summary.Total == 0 || result.Summary.NotCovered != result.Summary.Total {
		t.Errorf("summary = %+v, want every mutant not covered", result.Summary)
	}
}

func TestUnionHits(t *testing.T) {
	hit := func(names ...string) []runner.TestHit {
		var hs []runner.TestHit
		for _, n := range names {
			hs = append(hs, runner.TestHit{Name: n, Hits: 1})
		}
		return hs
	}
	got := unionHits(hit("TestA", "TestB"), hit("TestB", "TestC"))
	if want := hit("TestA", "TestB", "TestC"); !reflect.DeepEqual(got, want) {
		t.Errorf("unionHits = %v, want %v", got, want)
	}
	if got := unionHits(nil, nil); len(got) != 0 {
		t.Errorf("unionHits(nil, nil) = %v, want none", got)
	}
}

func TestRunCrossPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// The fixture is its own module. Only the tests of crosspkg/app and
	// crosspkg/reader run lib.Scale and lib.Half, and they never check
	// Half's result; reader's test must run in its own directory and leaves
	// a file there. orderdep's TestSecond fails on its own, mainfail's
	// TestMain fails and brokentest's tests do not compile.
	root := relDir(t, "../../internal/runner/testdata/crosspkg")
	chdir(t, root)
	t.Setenv("KANLY_CACHE_DIR", t.TempDir())
	type mutant struct {
		Mutation struct {
			Function string `json:"function"`
		} `json:"mutation"`
		Status        string   `json:"status"`
		CoveringTests []string `json:"covering_tests"`
		KillingTests  []string `json:"killing_tests"`
		Duration      int64    `json:"duration_ns"`
	}
	type result struct {
		Scope         string                `json:"scope"`
		CrossPackage  bool                  `json:"cross_package"`
		ExcludedTests []report.ExcludedTest `json:"excluded_tests"`
		Mutants       []mutant              `json:"mutants"`
	}
	var stderr bytes.Buffer
	runJSON := func(args ...string) result {
		t.Helper()
		var stdout bytes.Buffer
		stderr.Reset()
		if code := run(append([]string{"--format=json", "--operators=int_arith"}, args...), &stdout, &stderr); code != 0 {
			t.Fatalf("run %v returned %d; stderr: %s", args, code, stderr.String())
		}
		var r result
		if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
			t.Fatalf("parse JSON: %v\n%s", err, stdout.String())
		}
		return r
	}
	byFunc := func(r result) map[string]mutant {
		out := make(map[string]mutant)
		for _, m := range r.Mutants {
			out[m.Mutation.Function] = m
		}
		return out
	}

	ownRun := runJSON("./lib")
	if ownRun.CrossPackage {
		t.Error("cross_package set without --cross-package")
	}
	own := byFunc(ownRun)
	for _, fn := range []string{"Scale", "Half", "Unused"} {
		if got := own[fn].Status; got != "not_covered" {
			t.Errorf("own tests only: %s is %s, want not_covered", fn, got)
		}
	}

	crossRun := runJSON("--cross-package", "./lib")
	if !crossRun.CrossPackage || !strings.HasSuffix(crossRun.Scope, " --cross-package") {
		t.Errorf("cross_package = %v, scope = %q; want both to say --cross-package", crossRun.CrossPackage, crossRun.Scope)
	}
	all := byFunc(crossRun)
	if m := all["Add"]; m.Status != "killed" || !reflect.DeepEqual(m.KillingTests, []string{"TestAdd"}) {
		t.Errorf("Add: %+v, want killed by its own TestAdd", m)
	}
	if m := all["Scale"]; m.Status != "killed" || !reflect.DeepEqual(m.KillingTests, []string{"crosspkg/app.TestArea"}) || m.Duration <= 0 {
		t.Errorf("Scale: %+v, want killed by crosspkg/app.TestArea, with its run time", m)
	}
	if m := all["Half"]; m.Status != "survived" || !reflect.DeepEqual(m.CoveringTests, []string{"crosspkg/app.TestReport", "crosspkg/reader.TestLoad"}) {
		t.Errorf("Half: %+v, want survived, covered by app's TestReport and reader's TestLoad", m)
	}
	if got := all["Unused"].Status; got != "not_covered" {
		t.Errorf("Unused: %s, want not_covered", got)
	}
	wantExcluded := []report.ExcludedTest{{Package: "crosspkg/orderdep", Test: "TestSecond"}}
	if got := crossRun.ExcludedTests; len(got) != 1 || got[0].Package != wantExcluded[0].Package || got[0].Test != wantExcluded[0].Test || !strings.HasPrefix(got[0].Reason, "fails when run on its own") {
		t.Errorf("excluded_tests = %+v, want orderdep's TestSecond, which fails on its own", got)
	}
	// Dependents whose tests cannot run are left out, not fatal.
	for _, want := range []string{
		"not counting the tests of dependent crosspkg/mainfail: tests fail with no mutant applied",
		"not counting the tests of dependent crosspkg/brokentest: go test -c",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr lacks %q: %s", want, stderr.String())
		}
	}
	if _, err := os.Stat(filepath.Join(root, "reader", "scratch.txt")); !os.IsNotExist(err) {
		t.Errorf("reader's scratch.txt was left in its directory: %v", err)
	}
	const reused = "reusing the setup of dependent crosspkg/app"
	if strings.Contains(stderr.String(), reused) {
		t.Errorf("first --cross-package run reused a setup from an empty cache: %s", stderr.String())
	}

	// Nothing changed, so the next run reuses the clean setups, with the
	// same verdicts; --no-cache sets them up afresh.
	again := byFunc(runJSON("--cross-package", "./lib"))
	if !strings.Contains(stderr.String(), reused) || !strings.Contains(stderr.String(), "reusing the setup of dependent crosspkg/reader") {
		t.Errorf("second run did not reuse app's and reader's setups: %s", stderr.String())
	}
	if strings.Contains(stderr.String(), "reusing the setup of dependent crosspkg/orderdep") {
		t.Errorf("orderdep's setup, which excluded a test, was cached: %s", stderr.String())
	}
	for fn, want := range all {
		got := again[fn]
		got.Duration, want.Duration = 0, 0 // run times vary
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s with cached setups: %+v, want %+v", fn, got, want)
		}
	}
	runJSON("--cross-package", "--no-cache", "./lib")
	if strings.Contains(stderr.String(), reused) {
		t.Errorf("--no-cache reused a setup: %s", stderr.String())
	}

	// orderdep depends on app and lib; its excluded test is listed once.
	both := runJSON("--cross-package", "./lib", "./app")
	if n := len(both.ExcludedTests); n != 1 {
		t.Errorf("excluded_tests = %+v, want orderdep's TestSecond once", both.ExcludedTests)
	}
}
