package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

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
