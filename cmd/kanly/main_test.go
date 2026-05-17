package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
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

	if result.Summary.Total != 4 {
		t.Errorf("expected Total=4, got %d", result.Summary.Total)
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
