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

	"github.com/devenjarvis/cauldron/internal/mutation"
	"github.com/devenjarvis/cauldron/internal/report"
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
			Mutation: mutation.Mutation{ID: 1, Package: "example.com/pkg/foo", File: "foo.go", Line: 5, Column: 10, OperatorName: "int_arith", Original: "+", Mutant: "-"},
			Status:   mutation.StatusKilled,
			KillingTests: []string{"TestAdd"},
		},
		{
			Mutation: mutation.Mutation{ID: 2, Package: "example.com/pkg/foo", File: "foo.go", Line: 10, Column: 3, OperatorName: "int_arith", Original: "-", Mutant: "+"},
			Status:   mutation.StatusKilled,
			KillingTests: []string{"TestSub"},
		},
		{
			Mutation: mutation.Mutation{ID: 3, Package: "example.com/pkg/bar", File: "bar.go", Line: 3, Column: 15, OperatorName: "int_arith", Original: "*", Mutant: "/"},
			Status:   mutation.StatusSurvived,
		},
	}
}

func TestBuildAggregatesPerPackage(t *testing.T) {
	r := report.Build(makeResults())

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
	r := report.Build(results)

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

func TestWriteJSONStableFieldNames(t *testing.T) {
	results := makeResults()
	r := report.Build(results)

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
	for _, field := range []string{"mutation_id", "package", "file", "line", "column", "operator", "original", "mutant", "status", "killing_tests"} {
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

func TestWriteText(t *testing.T) {
	results := makeResults()
	r := report.Build(results)

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
}
