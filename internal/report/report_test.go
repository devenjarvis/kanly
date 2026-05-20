package report_test

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/report"
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

func makeResults() []mutation.Result {
	return []mutation.Result{
		{
			Mutation: mutation.Mutation{ID: 1, Package: "example.com/pkg/foo", File: "foo.go", Line: 5, Column: 10, Function: "Add", OperatorName: "int_arith", Original: "+", Mutant: "-"},
			Status:   mutation.StatusKilled,
			KillingTests: []string{"TestAdd"},
		},
		{
			Mutation: mutation.Mutation{ID: 2, Package: "example.com/pkg/foo", File: "foo.go", Line: 10, Column: 3, Function: "Sub", OperatorName: "int_arith", Original: "-", Mutant: "+"},
			Status:   mutation.StatusKilled,
			KillingTests: []string{"TestSub"},
		},
		{
			Mutation: mutation.Mutation{ID: 3, Package: "example.com/pkg/bar", File: "bar.go", Line: 3, Column: 15, Function: "Mul", OperatorName: "int_arith", Original: "*", Mutant: "/"},
			Status:   mutation.StatusSurvived,
		},
	}
}

func makeInventory() map[string][]string {
	return map[string][]string{
		"example.com/pkg/foo": {"TestAdd", "TestSub"},
		"example.com/pkg/bar": {"TestMul"},
	}
}

func TestBuildAggregatesPerPackage(t *testing.T) {
	r := report.Build(makeResults(), makeInventory())

	if len(r.Packages) != 2 {
		t.Fatalf("Packages: want 2, got %d", len(r.Packages))
	}

	// Packages must be sorted by import path: bar before foo.
	if r.Packages[0].Package != "example.com/pkg/bar" {
		t.Errorf("Packages[0].Package: want %q, got %q", "example.com/pkg/bar", r.Packages[0].Package)
	}
	if r.Packages[1].Package != "example.com/pkg/foo" {
		t.Errorf("Packages[1].Package: want %q, got %q", "example.com/pkg/foo", r.Packages[1].Package)
	}

	bar := r.Packages[0]
	if bar.Total != 1 {
		t.Errorf("bar Total: want 1, got %d", bar.Total)
	}
	if bar.Survived != 1 {
		t.Errorf("bar Survived: want 1, got %d", bar.Survived)
	}
	if bar.Score != 0 {
		t.Errorf("bar Score: want 0, got %v", bar.Score)
	}

	foo := r.Packages[1]
	if foo.Total != 2 {
		t.Errorf("foo Total: want 2, got %d", foo.Total)
	}
	if foo.Killed != 2 {
		t.Errorf("foo Killed: want 2, got %d", foo.Killed)
	}

	// Top-level summary unchanged.
	if r.Summary.Total != 3 {
		t.Errorf("Summary.Total: want 3, got %d", r.Summary.Total)
	}
}

func TestBuildComputesScore(t *testing.T) {
	results := makeResults()
	r := report.Build(results, makeInventory())

	if r.Summary.Total != 3 {
		t.Errorf("Total: want 3, got %d", r.Summary.Total)
	}
	if r.Summary.Killed != 2 {
		t.Errorf("Killed: want 2, got %d", r.Summary.Killed)
	}
	if r.Summary.Survived != 1 {
		t.Errorf("Survived: want 1, got %d", r.Summary.Survived)
	}

	wantScore := 2.0 / 3.0
	if math.Abs(r.Summary.Score-wantScore) > 1e-9 {
		t.Errorf("Score: want %v, got %v", wantScore, r.Summary.Score)
	}
}

func TestBuildAggregatesTestStats(t *testing.T) {
	r := report.Build(makeResults(), makeInventory())

	// Inventory has 3 tests total: TestAdd, TestSub (foo), TestMul (bar).
	if len(r.Tests) != 3 {
		t.Fatalf("Tests: want 3, got %d", len(r.Tests))
	}

	// Sort: KillCount desc, then package, then name.
	// TestAdd and TestSub each kill 1 mutant; TestMul kills 0.
	// Ties between TestAdd/TestSub: both in foo, so by name → TestAdd first.
	if r.Tests[0].Name != "TestAdd" || r.Tests[0].KillCount != 1 {
		t.Errorf("Tests[0]: want TestAdd/1, got %s/%d", r.Tests[0].Name, r.Tests[0].KillCount)
	}
	if r.Tests[1].Name != "TestSub" || r.Tests[1].KillCount != 1 {
		t.Errorf("Tests[1]: want TestSub/1, got %s/%d", r.Tests[1].Name, r.Tests[1].KillCount)
	}
	if r.Tests[2].Name != "TestMul" || r.Tests[2].KillCount != 0 {
		t.Errorf("Tests[2]: want TestMul/0, got %s/%d", r.Tests[2].Name, r.Tests[2].KillCount)
	}

	// Killed mutant IDs are recorded.
	if len(r.Tests[0].KilledMutants) != 1 || r.Tests[0].KilledMutants[0] != 1 {
		t.Errorf("TestAdd.KilledMutants: want [1], got %v", r.Tests[0].KilledMutants)
	}
}

func TestBuildZeroKillTests(t *testing.T) {
	r := report.Build(makeResults(), makeInventory())

	// TestMul is in inventory but appears in no KillingTests list.
	if len(r.ZeroKillTests) != 1 {
		t.Fatalf("ZeroKillTests: want 1, got %d (%v)", len(r.ZeroKillTests), r.ZeroKillTests)
	}
	if r.ZeroKillTests[0] != "example.com/pkg/bar.TestMul" {
		t.Errorf("ZeroKillTests[0]: want %q, got %q", "example.com/pkg/bar.TestMul", r.ZeroKillTests[0])
	}
}

func TestBuildRedundantTestGroups(t *testing.T) {
	results := []mutation.Result{
		{
			Mutation:     mutation.Mutation{ID: 1, Package: "p", File: "p.go", Line: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
			Status:       mutation.StatusKilled,
			KillingTests: []string{"TestA", "TestB"}, // Both kill mutant 1.
		},
		{
			Mutation:     mutation.Mutation{ID: 2, Package: "p", File: "p.go", Line: 2, OperatorName: "int_arith", Original: "*", Mutant: "/"},
			Status:       mutation.StatusKilled,
			KillingTests: []string{"TestA", "TestB", "TestC"}, // A and B both kill 1+2; C kills only 2.
		},
	}
	// Without C in KillingTests for mutant 1, C's kill-set is {2}; A and B share {1,2}.

	inventory := map[string][]string{"p": {"TestA", "TestB", "TestC"}}
	r := report.Build(results, inventory)

	if len(r.RedundantTestGroups) != 1 {
		t.Fatalf("RedundantTestGroups: want 1 group, got %d (%v)", len(r.RedundantTestGroups), r.RedundantTestGroups)
	}
	group := r.RedundantTestGroups[0]
	wantGroup := []string{"p.TestA", "p.TestB"}
	if len(group) != 2 || group[0] != wantGroup[0] || group[1] != wantGroup[1] {
		t.Errorf("RedundantTestGroups[0]: want %v, got %v", wantGroup, group)
	}
}

func TestBuildSurvivorsByFunction(t *testing.T) {
	r := report.Build(makeResults(), makeInventory())

	if len(r.SurvivorsByFunction) != 1 {
		t.Fatalf("SurvivorsByFunction: want 1 group, got %d", len(r.SurvivorsByFunction))
	}
	g := r.SurvivorsByFunction[0]
	if g.Package != "example.com/pkg/bar" || g.Function != "Mul" {
		t.Errorf("group: want bar/Mul, got %s/%s", g.Package, g.Function)
	}
	if len(g.Mutations) != 1 || g.Mutations[0].ID != 3 {
		t.Errorf("Mutations: want [id=3], got %v", g.Mutations)
	}
}

func TestWriteJSONStableFieldNames(t *testing.T) {
	results := makeResults()
	r := report.Build(results, makeInventory())

	var buf bytes.Buffer
	if err := report.WriteJSON(&buf, r); err != nil {
		t.Fatal(err)
	}

	goldenPath := relDir(t, "testdata/golden.json")

	// Golden update: if GOLDEN_UPDATE env var is set, write the golden file.
	if os.Getenv("GOLDEN_UPDATE") != "" {
		if err := os.WriteFile(goldenPath, buf.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
		t.Log("updated golden file")
		return
	}

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file: %v (run with GOLDEN_UPDATE=1 to create)", err)
	}

	// Check that the actual output contains all required field names.
	actual := buf.String()
	for _, field := range []string{
		"mutation_id", "package", "file", "line", "column", "function", "operator", "original", "mutant",
		"status", "killing_tests", "covering_tests",
		"tests", "kill_count", "killed_mutants", "zero_kill_tests", "redundant_test_groups", "survivors_by_function",
	} {
		if !strings.Contains(actual, `"`+field+`"`) {
			t.Errorf("output missing required field %q", field)
		}
	}

	// Deep-compare actual output against golden to catch schema regressions.
	var got, want interface{}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if err := json.Unmarshal(golden, &want); err != nil {
		t.Fatalf("golden is not valid JSON: %v", err)
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("output does not match golden\ngot:  %s\nwant: %s", gotJSON, wantJSON)
	}
}

func makeLLMResults() []mutation.Result {
	// A killed mutant, a survivor with one covering-but-not-killing test, and
	// a not-covered mutant — exercises every branch of the LLM renderer.
	return []mutation.Result{
		{
			Mutation:      mutation.Mutation{ID: 1, Package: "example.com/pkg/foo", File: "foo.go", Line: 5, Column: 10, Function: "Add", OperatorName: "int_arith", Original: "+", Mutant: "-"},
			Status:        mutation.StatusKilled,
			KillingTests:  []string{"TestAdd"},
			CoveringTests: []string{"TestAdd", "TestAddSmoke"},
		},
		{
			Mutation:      mutation.Mutation{ID: 2, Package: "example.com/pkg/foo", File: "foo.go", Line: 5, Column: 14, Function: "Add", OperatorName: "int_literal", Original: "1", Mutant: "0"},
			Status:        mutation.StatusSurvived,
			CoveringTests: []string{"TestAdd", "TestAddSmoke"},
		},
		{
			Mutation: mutation.Mutation{ID: 3, Package: "example.com/pkg/bar", File: "bar.go", Line: 3, Column: 15, Function: "Mul", OperatorName: "int_arith", Original: "*", Mutant: "/"},
			Status:   mutation.StatusNotCovered,
		},
	}
}

func TestWriteLLMGolden(t *testing.T) {
	results := makeLLMResults()
	inventory := map[string][]string{
		"example.com/pkg/foo": {"TestAdd", "TestAddSmoke"},
		"example.com/pkg/bar": {"TestMul"},
	}
	r := report.Build(results, inventory)
	r.Scope = "./..."

	// Fake source content keyed by file path, returned by an injectable
	// ReadFile so the golden is reproducible without a real filesystem.
	fakeFiles := map[string]string{
		"foo.go": "package foo\n\nfunc Add(a, b int) int {\n\t// no-op comment\n\treturn a + b + 1\n}\n",
		"bar.go": "package bar\n\nfunc Mul(a, b int) int { return a * b }\n",
	}
	src := report.LLMSource{
		FuncRanges: map[string]map[string]mutation.FuncRange{
			"example.com/pkg/foo": {
				"Add": {File: "foo.go", StartLine: 3, EndLine: 6},
			},
			"example.com/pkg/bar": {
				"Mul": {File: "bar.go", StartLine: 3, EndLine: 3},
			},
		},
		ReadFile: func(path string) ([]byte, error) {
			content, ok := fakeFiles[path]
			if !ok {
				return nil, os.ErrNotExist
			}
			return []byte(content), nil
		},
	}

	var buf bytes.Buffer
	if err := report.WriteLLM(&buf, r, src); err != nil {
		t.Fatal(err)
	}

	goldenPath := relDir(t, "testdata/golden.md")
	if os.Getenv("GOLDEN_UPDATE") != "" {
		if err := os.WriteFile(goldenPath, buf.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
		t.Log("updated golden file")
		return
	}

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file: %v (run with GOLDEN_UPDATE=1 to create)", err)
	}
	if buf.String() != string(golden) {
		t.Errorf("output does not match golden\n--- got ---\n%s\n--- want ---\n%s", buf.String(), string(golden))
	}
}

func TestWriteLLMEmpty(t *testing.T) {
	// Empty report should still render every section so the LLM sees the
	// full schema, even when there's nothing to act on.
	r := report.Build(nil, nil)
	var buf bytes.Buffer
	if err := report.WriteLLM(&buf, r, report.LLMSource{}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, section := range []string{
		"## Task",
		"## Summary",
		"## Live mutants",
		"## Uncovered mutants",
		"## Redundant test groups",
		"## Zero-kill tests",
		"## Test inventory",
	} {
		if !strings.Contains(out, section) {
			t.Errorf("missing section %q in empty report:\n%s", section, out)
		}
	}
	if !strings.Contains(out, "_None — every covered mutant was killed._") {
		t.Errorf("empty-live placeholder missing:\n%s", out)
	}
	if !strings.Contains(out, "_None — every mutation site is reached by at least one test._") {
		t.Errorf("empty-uncovered placeholder missing:\n%s", out)
	}
	// With no survivors there is nothing to iterate on, so the closing
	// section must NOT appear.
	if strings.Contains(out, "## Next iteration") {
		t.Errorf("empty report should not emit Next iteration section:\n%s", out)
	}
}

func TestWriteLLMMissingFuncRange(t *testing.T) {
	// When FuncRanges has no entry for a survivor's function, the renderer
	// should still emit the mutant entry — just without a snippet — instead
	// of panicking or skipping.
	results := []mutation.Result{
		{
			Mutation:      mutation.Mutation{ID: 1, Package: "p", File: "p.go", Line: 5, Column: 1, Function: "Mystery", OperatorName: "int_arith", Original: "+", Mutant: "-"},
			Status:        mutation.StatusSurvived,
			CoveringTests: []string{"TestX"},
		},
	}
	r := report.Build(results, map[string][]string{"p": {"TestX"}})
	var buf bytes.Buffer
	if err := report.WriteLLM(&buf, r, report.LLMSource{}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "**#1**") {
		t.Errorf("mutant entry missing:\n%s", out)
	}
	if strings.Contains(out, "```go") {
		t.Errorf("snippet should not appear when no FuncRange is provided:\n%s", out)
	}
}

func TestWriteText(t *testing.T) {
	results := makeResults()
	r := report.Build(results, makeInventory())

	var buf bytes.Buffer
	if err := report.WriteText(&buf, r); err != nil {
		t.Fatal(err)
	}

	out := buf.String()

	// Survived mutants must appear before the summary line.
	if !strings.Contains(out, "bar.go:3:15 [int_arith] *→/") {
		t.Errorf("survived mutant line missing or malformed:\n%s", out)
	}

	// Killed mutants must NOT appear in the output body (only survived are actionable).
	if strings.Contains(out, "foo.go:5:10") {
		t.Errorf("killed mutant should not appear in text output:\n%s", out)
	}

	// Per-package summary lines must appear.
	if !strings.Contains(out, "Package: example.com/pkg/foo | Total: 2 | Killed: 2 | Survived: 0 | Score: 100.0%") {
		t.Errorf("per-package foo summary missing or malformed:\n%s", out)
	}
	if !strings.Contains(out, "Package: example.com/pkg/bar | Total: 1 | Killed: 0 | Survived: 1 | Score: 0.0%") {
		t.Errorf("per-package bar summary missing or malformed:\n%s", out)
	}

	// Per-package lines must appear before the aggregate footer.
	fooIdx := strings.Index(out, "Package: example.com/pkg/foo")
	barIdx := strings.Index(out, "Package: example.com/pkg/bar")
	totalIdx := strings.Index(out, "Total: 3")
	if fooIdx == -1 || barIdx == -1 || totalIdx == -1 {
		t.Fatalf("missing lines; fooIdx=%d barIdx=%d totalIdx=%d\n%s", fooIdx, barIdx, totalIdx, out)
	}
	if fooIdx > totalIdx || barIdx > totalIdx {
		t.Errorf("per-package lines must appear before the aggregate footer:\n%s", out)
	}

	// Aggregate footer must be present.
	if !strings.Contains(out, "Total: 3") {
		t.Errorf("summary missing Total:\n%s", out)
	}
	if !strings.Contains(out, "Killed: 2") {
		t.Errorf("summary missing Killed:\n%s", out)
	}
	if !strings.Contains(out, "Survived: 1") {
		t.Errorf("summary missing Survived:\n%s", out)
	}

	// Top killers section: TestAdd (1) and TestSub (1) are non-zero killers.
	if !strings.Contains(out, "Top tests by kill count:") {
		t.Errorf("missing 'Top tests by kill count:' section:\n%s", out)
	}
	if !strings.Contains(out, "example.com/pkg/foo.TestAdd (1)") {
		t.Errorf("top killers section missing TestAdd:\n%s", out)
	}

	// Zero-kill section: TestMul (bar) is in inventory but kills nothing.
	if !strings.Contains(out, "Tests that killed nothing:") {
		t.Errorf("missing 'Tests that killed nothing:' section:\n%s", out)
	}
	if !strings.Contains(out, "example.com/pkg/bar.TestMul") {
		t.Errorf("zero-kill section missing TestMul:\n%s", out)
	}
}
