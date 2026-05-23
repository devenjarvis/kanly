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
		Scope    string `json:"scope"`
		Summary  struct {
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

// TestIncidentalCoverageDroppedFromScopedRun verifies the diff-line incidental
// filter end-to-end. The incidentalsample fixture has TestAddTable (3 hits on
// Add's return line) and TestHelperBehavior (1 hit, only via Helper's setup
// call). When scoped to :Add, the filter must:
//   - drop TestHelperBehavior from the Add mutant's CoveringTests (because
//     maxHits == 3 > 1 and TestHelperBehavior touches only one scoped line
//     with hits == 1),
//   - surface it in incidental_coverage_tests instead of zero_kill_tests.
func TestIncidentalCoverageDroppedFromScopedRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	sampleDir := relDir(t, "../../internal/runner/testdata/incidentalsample")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format=json", sampleDir + ":Add"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
	}

	var result struct {
		ZeroKillTests           []string `json:"zero_kill_tests"`
		IncidentalCoverageTests []string `json:"incidental_coverage_tests"`
		Mutants                 []struct {
			Mutation struct {
				Function string `json:"function"`
			} `json:"mutation"`
			Status        string   `json:"status"`
			CoveringTests []string `json:"covering_tests"`
			KillingTests  []string `json:"killing_tests"`
		} `json:"mutants"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout.String())
	}

	const wantIncidental = "github.com/devenjarvis/kanly/internal/runner/testdata/incidentalsample.TestHelperBehavior"
	if !containsString(result.IncidentalCoverageTests, wantIncidental) {
		t.Errorf("incidental_coverage_tests: want %q in %v", wantIncidental, result.IncidentalCoverageTests)
	}
	if containsString(result.ZeroKillTests, wantIncidental) {
		t.Errorf("zero_kill_tests must not contain incidental test %q; got %v", wantIncidental, result.ZeroKillTests)
	}

	// The single Add mutation should have run only against TestAddTable.
	if len(result.Mutants) != 1 {
		t.Fatalf("expected exactly 1 mutation under :Add scope, got %d", len(result.Mutants))
	}
	m := result.Mutants[0]
	if m.Mutation.Function != "Add" {
		t.Errorf("mutant function: want Add, got %q", m.Mutation.Function)
	}
	if !equalStringsSorted(m.CoveringTests, []string{"TestAddTable"}) {
		t.Errorf("covering_tests: want [TestAddTable], got %v", m.CoveringTests)
	}
}

// TestIncidentalFilterInactiveOnFullRun is the regression guard for the
// "applied only when a scope is active" invariant: on a full run, the
// incidental filter must NOT fire, and incidental_coverage_tests must be
// omitted from the JSON entirely.
func TestIncidentalFilterInactiveOnFullRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	sampleDir := relDir(t, "../../internal/runner/testdata/incidentalsample")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format=json", sampleDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
	}

	if strings.Contains(stdout.String(), "incidental_coverage_tests") {
		t.Errorf("full run JSON must omit incidental_coverage_tests key, got:\n%s", stdout.String())
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
			name:    "spec and tests together",
			specs:   []selector.Spec{{Pattern: "./..."}},
			tests:   "^TestFoo$",
			want:    "./... --tests=^TestFoo$",
		},
		{
			name:    "diff with custom base",
			diff:    true,
			diffBase: "origin/main",
			want:    "--diff=origin/main",
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
