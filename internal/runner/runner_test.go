package runner_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/devenjarvis/cauldron/internal/mutation"
	"github.com/devenjarvis/cauldron/internal/operators"
	"github.com/devenjarvis/cauldron/internal/runner"
	"github.com/devenjarvis/cauldron/internal/schema"
	"github.com/devenjarvis/cauldron/internal/source"
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

func TestRunMutantKillsAndSurvives(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	sampleDir := relDir(t, "testdata/sample")
	pkg, err := source.Load(sampleDir)
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	ops := []mutation.Operator{operators.IntArith{}}
	rew, err := schema.Rewrite(pkg, ops)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	if len(rew.Mutations) != 2 {
		t.Fatalf("expected 2 mutations (Add +→- and Sub -→+), got %d: %v", len(rew.Mutations), rew.Mutations)
	}

	overlayPath, cleanup, err := runner.BuildOverlay(rew, sampleDir)
	if err != nil {
		t.Fatalf("BuildOverlay: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	binaryPath, cleanBin, err := runner.CompileTestBinary(ctx, pkg.ImportPath, overlayPath)
	if err != nil {
		t.Fatalf("CompileTestBinary: %v", err)
	}
	defer cleanBin()

	timeout := 30 * time.Second

	// Mutation 1: Add +→-. TestAdd expects Add(2,3)==5; with mutant Add returns -1. Killed.
	status1, _, _, err := runner.RunMutant(ctx, binaryPath, rew.Mutations[0].ID, timeout)
	if err != nil {
		t.Fatalf("RunMutant(1): %v", err)
	}
	if status1 != mutation.StatusKilled {
		t.Errorf("mutation 1 (%s→%s): expected Killed, got %s",
			rew.Mutations[0].Original, rew.Mutations[0].Mutant, status1)
	}

	// Mutation 2: Sub -→+. TestSub only checks non-zero; with mutant Sub(5,3)=8!=0. Survived.
	status2, _, _, err := runner.RunMutant(ctx, binaryPath, rew.Mutations[1].ID, timeout)
	if err != nil {
		t.Fatalf("RunMutant(2): %v", err)
	}
	if status2 != mutation.StatusSurvived {
		t.Errorf("mutation 2 (%s→%s): expected Survived, got %s",
			rew.Mutations[1].Original, rew.Mutations[1].Mutant, status2)
	}
}
