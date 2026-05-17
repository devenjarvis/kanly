package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
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

func TestEndToEndSamplePackage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Build the CLI binary.
	tmpDir, err := os.MkdirTemp("", "cauldron-e2e-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	binPath := filepath.Join(tmpDir, "cauldron")
	moduleRoot := relDir(t, "../..")

	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/cauldron")
	buildCmd.Dir = moduleRoot
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

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

	// Verify the killed mutant is Add's +→- and the killing test is TestAdd.
	for _, m := range result.Mutants {
		if m.Status == "killed" {
			if m.Mutation.Original != "+" || m.Mutation.Mutant != "-" {
				t.Errorf("expected killed mutant +→-, got %s→%s", m.Mutation.Original, m.Mutation.Mutant)
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
		}
	}
}
