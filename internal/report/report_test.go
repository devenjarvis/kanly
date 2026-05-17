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
			Mutation: mutation.Mutation{ID: 1, File: "foo.go", Line: 5, Column: 10, OperatorName: "int_arith", Original: "+", Mutant: "-"},
			Status:   mutation.StatusKilled,
			KillingTests: []string{"TestAdd"},
		},
		{
			Mutation: mutation.Mutation{ID: 2, File: "foo.go", Line: 10, Column: 3, OperatorName: "int_arith", Original: "-", Mutant: "+"},
			Status:   mutation.StatusKilled,
			KillingTests: []string{"TestSub"},
		},
		{
			Mutation: mutation.Mutation{ID: 3, File: "bar.go", Line: 3, Column: 15, OperatorName: "int_arith", Original: "*", Mutant: "/"},
			Status:   mutation.StatusSurvived,
		},
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
	for _, field := range []string{"mutation_id", "file", "line", "column", "operator", "original", "mutant", "status", "killing_tests"} {
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

	// Summary footer must be present.
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
