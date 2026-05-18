package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "kanly-e2e-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	binPath := filepath.Join(tmpDir, "kanly")
	moduleRoot := relDir(t, "../..")
	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/kanly")
	buildCmd.Dir = moduleRoot
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return binPath
}

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

func TestEndToEndSamplePackage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binPath := buildBinary(t)

	sampleDir := relDir(t, "../runner/testdata/sample")

	runCmd := exec.Command(binPath, "--format=json", sampleDir)
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kanly run: %v\n%s", err, out)
	}

	var result struct {
		Summary struct {
			Total    int     `json:"total"`
			Killed   int     `json:"killed"`
			Survived int     `json:"survived"`
			Score    float64 `json:"score"`
		} `json:"summary"`
		Mutants []struct {
			Mutation struct {
				ID       int    `json:"mutation_id"`
				Original string `json:"original"`
				Mutant   string `json:"mutant"`
			} `json:"mutation"`
			Status       string   `json:"status"`
			KillingTests []string `json:"killing_tests"`
		} `json:"mutants"`
	}

	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, out)
	}

	// Pinned ledger: sample has Add(+→-) killed by TestAdd, Sub(-→+) survived (weak test).
	wantTotal := 2
	wantKilled := 1
	wantSurvived := 1

	if result.Summary.Total != wantTotal {
		t.Errorf("Total: want %d, got %d", wantTotal, result.Summary.Total)
	}
	if result.Summary.Killed != wantKilled {
		t.Errorf("Killed: want %d, got %d", wantKilled, result.Summary.Killed)
	}
	if result.Summary.Survived != wantSurvived {
		t.Errorf("Survived: want %d, got %d", wantSurvived, result.Summary.Survived)
	}

	// Pinned ledger: verify each mutant's details.
	for _, m := range result.Mutants {
		switch m.Status {
		case "killed":
			if m.Mutation.Original != "+" || m.Mutation.Mutant != "-" {
				t.Errorf("killed mutant: expected +→-, got %s→%s", m.Mutation.Original, m.Mutation.Mutant)
			}
			found := false
			for _, kt := range m.KillingTests {
				if kt == "TestAdd" {
					found = true
				}
			}
			if !found {
				t.Errorf("expected TestAdd in killing tests, got %v", m.KillingTests)
			}
		case "survived":
			if m.Mutation.Original != "-" || m.Mutation.Mutant != "+" {
				t.Errorf("survived mutant: expected -→+, got %s→%s", m.Mutation.Original, m.Mutation.Mutant)
			}
			if len(m.KillingTests) != 0 {
				t.Errorf("survived mutant should have no killing tests, got %v", m.KillingTests)
			}
		}
	}

	// Score must reflect 1 killed out of 2 total (= 0.5).
	wantScore := 0.5
	if result.Summary.Score != wantScore {
		t.Errorf("Score: want %v, got %v", wantScore, result.Summary.Score)
	}
}

func TestEndToEndMultiPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binPath := buildBinary(t)

	multipkgDir := relDir(t, "../runner/testdata/multipkg")

	runCmd := exec.Command(binPath, "--format=json", "./...")
	runCmd.Dir = multipkgDir
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kanly run: %v\n%s", err, out)
	}

	var result struct {
		Summary struct {
			Total    int `json:"total"`
			Killed   int `json:"killed"`
			Survived int `json:"survived"`
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

	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, out)
	}

	// Pinned ledger:
	//   foo has 2 mutations (Add +→-, Sub -→+).
	//   bar has 4: int_cmp_boundary >→>=, int_cmp_negate >→<=,
	//              and int_literal 0→1, 0→-1 on the `0` in `n > 0`.
	// TestAdd kills Add; TestSub is weak so Sub survives. The bar mutations
	// all die to TestIsPositive's three-point check.
	if result.Summary.Total != 6 {
		t.Errorf("Total: want 6, got %d", result.Summary.Total)
	}
	if result.Summary.Killed != 5 {
		t.Errorf("Killed: want 5, got %d", result.Summary.Killed)
	}
	if result.Summary.Survived != 1 {
		t.Errorf("Survived: want 1, got %d", result.Summary.Survived)
	}

	if len(result.Packages) != 2 {
		t.Errorf("Packages: want 2, got %d", len(result.Packages))
	}

	// The fixture's go.mod declares "module multipkg", so the full import paths
	// are "multipkg/foo" and "multipkg/bar" — no hostname prefix.  The module
	// has no external dependencies, so no go.sum is required.
	wantPkgs := map[string]bool{"multipkg/foo": true, "multipkg/bar": true}
	for _, p := range result.Packages {
		if !wantPkgs[p.Package] {
			t.Errorf("unexpected package %q in Packages", p.Package)
		}
	}

	for i, m := range result.Mutants {
		if m.Mutation.Package == "" {
			t.Errorf("Mutants[%d].Mutation.Package is empty", i)
		}
		if !wantPkgs[m.Mutation.Package] {
			t.Errorf("Mutants[%d].Mutation.Package %q is not one of the expected packages", i, m.Mutation.Package)
		}
	}
}

// TestEndToEndDiff verifies that `kanly --diff` filters mutations to only those
// on lines touched by the diff. A tmp git repo holds a two-function sample;
// only the `Sub` line is modified, so only the Sub mutation should run.
func TestEndToEndDiff(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}

	binPath := buildBinary(t)
	repo := t.TempDir()

	goMod := "module diffsample\n\ngo 1.23.0\n"
	sampleGo := `package diffsample

func Add(a, b int) int { return a + b }

func Sub(a, b int) int { return a - b }
`
	sampleTest := `package diffsample

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatal("Add(2,3) != 5")
	}
}

func TestSub(t *testing.T) {
	if Sub(5, 3) != 2 {
		t.Fatal("Sub(5,3) != 2")
	}
}
`
	mustWrite := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("go.mod", goMod)
	mustWrite("sample.go", sampleGo)
	mustWrite("sample_test.go", sampleTest)

	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@e.x",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@e.x",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q", "-b", "main")
	runGit("config", "commit.gpgsign", "false")
	runGit("add", ".")
	runGit("commit", "-q", "-m", "init")

	// Modify only the line containing `Sub`. The change keeps the function
	// behaviour identical but the diff covers line 5.
	modified := strings.Replace(sampleGo, "return a - b", "return (a - b)", 1)
	mustWrite("sample.go", modified)

	runCmd := exec.Command(binPath, "--diff", "--format=json")
	runCmd.Dir = repo
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kanly --diff: %v\n%s", err, out)
	}

	var result struct {
		Summary struct {
			Total int `json:"total"`
		} `json:"summary"`
		Mutants []struct {
			Mutation struct {
				Line     int    `json:"line"`
				Original string `json:"original"`
				Mutant   string `json:"mutant"`
			} `json:"mutation"`
		} `json:"mutants"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, out)
	}

	// Only mutations on line 5 should be present; Add (line 3) is outside the
	// diff. Two operators target line 5: int_arith on `a - b` and return_zero
	// on the surrounding `(a - b)` ParenExpr.
	if result.Summary.Total != 2 {
		t.Fatalf("Total: want 2, got %d\noutput:\n%s", result.Summary.Total, out)
	}
	var sawArith, sawRetZero bool
	for _, mm := range result.Mutants {
		if mm.Mutation.Line != 5 {
			t.Errorf("mutant line: want 5, got %d", mm.Mutation.Line)
		}
		switch {
		case mm.Mutation.Original == "-" && mm.Mutation.Mutant == "+":
			sawArith = true
		case mm.Mutation.Original == "value" && mm.Mutation.Mutant == "0":
			sawRetZero = true
		}
	}
	if !sawArith {
		t.Errorf("missing int_arith -→+ mutant on line 5")
	}
	if !sawRetZero {
		t.Errorf("missing return_zero value→0 mutant on line 5")
	}
}

// TestEndToEndDiffEmpty verifies that `kanly --diff` with no changes exits 0
// and produces an empty report.
func TestEndToEndDiffEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}

	binPath := buildBinary(t)
	repo := t.TempDir()

	mustWrite := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("go.mod", "module emptysample\n\ngo 1.23.0\n")
	mustWrite("sample.go", "package emptysample\n\nfunc Add(a, b int) int { return a + b }\n")
	mustWrite("sample_test.go", "package emptysample\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { _ = Add(1, 2) }\n")

	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@e.x",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@e.x",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q", "-b", "main")
	runGit("config", "commit.gpgsign", "false")
	runGit("add", ".")
	runGit("commit", "-q", "-m", "init")

	runCmd := exec.Command(binPath, "--diff", "--format=json")
	runCmd.Dir = repo
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kanly --diff: %v\n%s", err, out)
	}

	var result struct {
		Summary struct {
			Total int `json:"total"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, out)
	}
	if result.Summary.Total != 0 {
		t.Errorf("Total: want 0, got %d", result.Summary.Total)
	}
}

// TestEndToEndUncovered verifies that mutants on lines with no test coverage
// are emitted as not_covered (no test run) and excluded from the mutation
// score's denominator, while covered mutants on the same package run normally.
func TestEndToEndUncovered(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binPath := buildBinary(t)
	uncoveredDir := relDir(t, "../runner/testdata/uncoveredsample")

	runCmd := exec.Command(binPath, "--format=json", uncoveredDir)
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kanly run: %v\n%s", err, out)
	}

	var result struct {
		Summary struct {
			Total      int     `json:"total"`
			Killed     int     `json:"killed"`
			Survived   int     `json:"survived"`
			NotCovered int     `json:"not_covered"`
			Score      float64 `json:"score"`
		} `json:"summary"`
		Mutants []struct {
			Mutation struct {
				Function string `json:"function"`
			} `json:"mutation"`
			Status string `json:"status"`
		} `json:"mutants"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, out)
	}

	// Live has one + mutant (covered by TestLive, killed).
	// Dead has one - mutant (no covering test, not_covered).
	if result.Summary.Total != 2 {
		t.Errorf("Total: want 2, got %d", result.Summary.Total)
	}
	if result.Summary.NotCovered != 1 {
		t.Errorf("NotCovered: want 1, got %d", result.Summary.NotCovered)
	}
	if result.Summary.Killed != 1 {
		t.Errorf("Killed: want 1, got %d", result.Summary.Killed)
	}
	// Score must be killed / (total - not_covered - not_viable) = 1 / 1 = 1.0.
	if result.Summary.Score != 1.0 {
		t.Errorf("Score: want 1.0, got %v", result.Summary.Score)
	}

	for _, m := range result.Mutants {
		switch m.Mutation.Function {
		case "Live":
			if m.Status != "killed" {
				t.Errorf("Live mutant: want killed, got %s", m.Status)
			}
		case "Dead":
			if m.Status != "not_covered" {
				t.Errorf("Dead mutant: want not_covered, got %s", m.Status)
			}
		}
	}
}

func TestEndToEndBooleanPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binPath := buildBinary(t)
	boolDir := relDir(t, "../runner/testdata/boolsample")

	runCmd := exec.Command(binPath, "--format=json", boolDir)
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kanly run: %v\n%s", err, out)
	}

	var result struct {
		Summary struct {
			Total    int `json:"total"`
			Killed   int `json:"killed"`
			Survived int `json:"survived"`
		} `json:"summary"`
		Mutants []struct {
			Mutation struct {
				Operator string `json:"operator"`
			} `json:"mutation"`
		} `json:"mutants"`
	}

	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, out)
	}

	// Pinned ledger: boolsample has exactly 3 bool mutations, all killed.
	if result.Summary.Total != 3 {
		t.Errorf("Total: want 3, got %d", result.Summary.Total)
	}
	if result.Summary.Killed != 3 {
		t.Errorf("Killed: want 3, got %d", result.Summary.Killed)
	}
	if result.Summary.Survived != 0 {
		t.Errorf("Survived: want 0, got %d", result.Summary.Survived)
	}

	logicCount := 0
	notCount := 0
	for _, m := range result.Mutants {
		switch m.Mutation.Operator {
		case "bool_logic":
			logicCount++
		case "bool_not":
			notCount++
		}
	}
	if logicCount != 2 {
		t.Errorf("bool_logic mutant count: want 2, got %d", logicCount)
	}
	if notCount != 1 {
		t.Errorf("bool_not mutant count: want 1, got %d", notCount)
	}
}
