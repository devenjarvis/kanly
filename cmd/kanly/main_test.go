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

func TestParseMutantIDs(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		want            map[int]bool
		wantErrContains string
	}{
		{name: "empty", input: "", want: nil},
		{name: "whitespace only", input: "  ", want: nil},
		{name: "single id", input: "1", want: map[int]bool{1: true}},
		{name: "multiple ids", input: "1,2,3", want: map[int]bool{1: true, 2: true, 3: true}},
		{name: "spaces around ids", input: " 2 , 3 ", want: map[int]bool{2: true, 3: true}},
		{name: "zero is invalid", input: "0", wantErrContains: "must be >= 1"},
		{name: "negative is invalid", input: "-1", wantErrContains: "must be >= 1"},
		{name: "non-integer", input: "abc", wantErrContains: "invalid id"},
		{name: "empty part from comma", input: ",", wantErrContains: "empty id"},
		{name: "trailing comma", input: "1,", wantErrContains: "empty id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMutantIDs(tt.input)
			if tt.wantErrContains != "" {
				if err == nil {
					t.Fatalf("parseMutantIDs(%q): expected error containing %q, got nil", tt.input, tt.wantErrContains)
				}
				if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("parseMutantIDs(%q): error %q does not contain %q", tt.input, err.Error(), tt.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMutantIDs(%q): unexpected error: %v", tt.input, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseMutantIDs(%q): want %v, got %v", tt.input, tt.want, got)
			}
		})
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
	for _, args := range [][]string{{"--timeout=-1s", "."}, {"--timeout-factor=0.5", "."}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("run(%v) = %d, want 2; stderr: %s", args, code, stderr.String())
		}
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
	fake := func(next map[int]mutation.Status, calls *[]call) func(mutation.Result, float64) (mutation.Result, error) {
		return func(r mutation.Result, scale float64) (mutation.Result, error) {
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
