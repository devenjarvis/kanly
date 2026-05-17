package runner_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestCompileTestBinaryErrorDoesNotPanic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: invokes go toolchain")
	}
	ctx := context.Background()
	// Pass a nonexistent overlay — go test -c will fail immediately.
	_, cleanup, err := runner.CompileTestBinary(ctx, "github.com/devenjarvis/cauldron/internal/runner", "/nonexistent/overlay.json")
	if err == nil {
		t.Fatal("expected error for bad overlay, got nil")
	}
	if cleanup != nil {
		t.Errorf("expected nil cleanup on error, got non-nil")
	}
}

func TestBuildOverlay(t *testing.T) {
	pkgDir := t.TempDir()
	srcFile := filepath.Join(pkgDir, "foo.go")
	fakeContent := "package foo\nfunc Foo() {}\n"

	rew := &schema.Rewritten{
		Files:      map[string]string{srcFile: fakeContent},
		Dispatcher: "package foo\n",
	}

	overlayPath, cleanup, err := runner.BuildOverlay(rew, pkgDir)
	if err != nil {
		t.Fatalf("BuildOverlay: %v", err)
	}
	defer cleanup()

	data, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatalf("read overlay JSON: %v", err)
	}
	var overlay struct {
		Replace map[string]string `json:"Replace"`
	}
	if err := json.Unmarshal(data, &overlay); err != nil {
		t.Fatalf("parse overlay JSON: %v", err)
	}

	// Expect 2 entries: rewritten source file + dispatcher.
	if len(overlay.Replace) != 2 {
		t.Errorf("expected 2 overlay entries, got %d: %v", len(overlay.Replace), overlay.Replace)
	}

	// Source file entry: key is absolute original path, value is a readable tmp file.
	tmpSrc, ok := overlay.Replace[srcFile]
	if !ok {
		t.Errorf("overlay missing entry for %s; keys: %v", srcFile, overlay.Replace)
	} else {
		got, err := os.ReadFile(tmpSrc)
		if err != nil {
			t.Fatalf("read rewritten file: %v", err)
		}
		if string(got) != fakeContent {
			t.Errorf("rewritten content: want %q, got %q", fakeContent, string(got))
		}
	}

	// Dispatcher entry: key is pkgDir/cauldron_schema.go.
	dispKey := filepath.Join(pkgDir, "cauldron_schema.go")
	tmpDisp, ok := overlay.Replace[dispKey]
	if !ok {
		t.Errorf("overlay missing dispatcher entry for %s; keys: %v", dispKey, overlay.Replace)
	} else {
		got, err := os.ReadFile(tmpDisp)
		if err != nil {
			t.Fatalf("read dispatcher file: %v", err)
		}
		if string(got) != rew.Dispatcher {
			t.Errorf("dispatcher content mismatch")
		}
	}
}

func TestBuildOverlayCollisionSafe(t *testing.T) {
	// Two source files in different directories but with the same base name.
	pkgDir := t.TempDir()
	file1 := filepath.Join(pkgDir, "a", "foo.go")
	file2 := filepath.Join(pkgDir, "b", "foo.go")

	rew := &schema.Rewritten{
		Files: map[string]string{
			file1: "package a\n",
			file2: "package b\n",
		},
		Dispatcher: "package x\n",
	}

	overlayPath, cleanup, err := runner.BuildOverlay(rew, pkgDir)
	if err != nil {
		t.Fatalf("BuildOverlay: %v", err)
	}
	defer cleanup()

	data, _ := os.ReadFile(overlayPath)
	var overlay struct {
		Replace map[string]string `json:"Replace"`
	}
	if err := json.Unmarshal(data, &overlay); err != nil {
		t.Fatal(err)
	}

	// 3 entries: 2 source files + dispatcher.
	if len(overlay.Replace) != 3 {
		t.Errorf("expected 3 overlay entries, got %d", len(overlay.Replace))
	}

	// The temp paths for same-named source files must be distinct.
	var tmpPaths []string
	for k, v := range overlay.Replace {
		if strings.HasSuffix(k, "foo.go") {
			tmpPaths = append(tmpPaths, v)
		}
	}
	if len(tmpPaths) == 2 && tmpPaths[0] == tmpPaths[1] {
		t.Error("same-basename files collide in overlay temp dir")
	}
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
	status1, _, _, err := runner.RunMutant(ctx, binaryPath, rew.Mutations[0].ID, sampleDir, timeout)
	if err != nil {
		t.Fatalf("RunMutant(1): %v", err)
	}
	if status1 != mutation.StatusKilled {
		t.Errorf("mutation 1 (%s→%s): expected Killed, got %s",
			rew.Mutations[0].Original, rew.Mutations[0].Mutant, status1)
	}

	// Mutation 2: Sub -→+. TestSub only checks non-zero; with mutant Sub(5,3)=8!=0. Survived.
	status2, _, _, err := runner.RunMutant(ctx, binaryPath, rew.Mutations[1].ID, sampleDir, timeout)
	if err != nil {
		t.Fatalf("RunMutant(2): %v", err)
	}
	if status2 != mutation.StatusSurvived {
		t.Errorf("mutation 2 (%s→%s): expected Survived, got %s",
			rew.Mutations[1].Original, rew.Mutations[1].Mutant, status2)
	}
}

func TestRunMutantKillsBooleanMutants(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	boolDir := relDir(t, "testdata/boolsample")
	pkg, err := source.Load(boolDir)
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	ops := []mutation.Operator{operators.BoolLogic{}, operators.BoolNot{}}
	rew, err := schema.Rewrite(pkg, ops)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	// Both() has &&→||, Either() has ||→&&, Negate() has !→removal → 3 mutations total.
	if len(rew.Mutations) != 3 {
		t.Fatalf("expected 3 mutations, got %d: %v", len(rew.Mutations), rew.Mutations)
	}

	overlayPath, cleanup, err := runner.BuildOverlay(rew, boolDir)
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

	for _, m := range rew.Mutations {
		status, _, _, err := runner.RunMutant(ctx, binaryPath, m.ID, boolDir, timeout)
		if err != nil {
			t.Fatalf("RunMutant(%d, %s→%s): %v", m.ID, m.Original, m.Mutant, err)
		}
		if status != mutation.StatusKilled {
			t.Errorf("mutation %d (%s %s→%q): expected Killed, got %s",
				m.ID, m.OperatorName, m.Original, m.Mutant, status)
		}
	}
}

func TestRunMutantKillsComparisonMutants(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cmpDir := relDir(t, "testdata/cmpsample")
	pkg, err := source.Load(cmpDir)
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	ops := []mutation.Operator{operators.IntCmpBoundary{}, operators.IntCmpNegate{}}
	rew, err := schema.Rewrite(pkg, ops)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	// IsPositive has one '>' — one boundary mutant (>→>=) and one negate mutant (>→<=).
	if len(rew.Mutations) != 2 {
		t.Fatalf("expected 2 mutations, got %d: %v", len(rew.Mutations), rew.Mutations)
	}

	overlayPath, cleanup, err := runner.BuildOverlay(rew, cmpDir)
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

	for _, m := range rew.Mutations {
		status, _, _, err := runner.RunMutant(ctx, binaryPath, m.ID, cmpDir, timeout)
		if err != nil {
			t.Fatalf("RunMutant(%d, %s→%s): %v", m.ID, m.Original, m.Mutant, err)
		}
		if status != mutation.StatusKilled {
			t.Errorf("mutation %d (%s %s→%s): expected Killed, got %s",
				m.ID, m.OperatorName, m.Original, m.Mutant, status)
		}
	}
}
