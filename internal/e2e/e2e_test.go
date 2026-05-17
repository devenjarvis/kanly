package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "cauldron-e2e-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	binPath := filepath.Join(tmpDir, "cauldron")
	moduleRoot := relDir(t, "../..")
	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/cauldron")
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
		t.Fatalf("cauldron run: %v\n%s", err, out)
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
		t.Fatalf("cauldron run: %v\n%s", err, out)
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

	// Pinned ledger: foo has 2 mutations (Add +→-, Sub -→+), bar has 2 (>→>= and >→<=).
	// TestAdd kills Add mutation; TestSub is weak (non-zero check) so Sub survives.
	// All bar mutations killed by precise IsPositive tests.
	if result.Summary.Total != 4 {
		t.Errorf("Total: want 4, got %d", result.Summary.Total)
	}
	if result.Summary.Killed != 3 {
		t.Errorf("Killed: want 3, got %d", result.Summary.Killed)
	}
	if result.Summary.Survived != 1 {
		t.Errorf("Survived: want 1, got %d", result.Summary.Survived)
	}

	if len(result.Packages) != 2 {
		t.Errorf("Packages: want 2, got %d", len(result.Packages))
	}

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
