package operators_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/devenjarvis/cauldron/internal/operators"
	"github.com/devenjarvis/cauldron/internal/source"
)

// relDir returns the absolute path of sub relative to this test file's directory.
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

func TestIntArithFindsAddOnly(t *testing.T) {
	pkg, err := source.Load(relDir(t, "../source/testdata/simple"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	op := operators.IntArith{}
	var candidates []interface{}
	for _, f := range pkg.Files {
		cs := op.Find(f, pkg.TypesInfo)
		for _, c := range cs {
			candidates = append(candidates, c)
			if c.Original != "+" {
				t.Errorf("expected Original=+, got %q", c.Original)
			}
			if c.Mutant != "-" {
				t.Errorf("expected Mutant=-, got %q", c.Mutant)
			}
		}
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
}

func TestIntArithSkipsFloat64(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/floatpkg"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	op := operators.IntArith{}
	for _, f := range pkg.Files {
		cs := op.Find(f, pkg.TypesInfo)
		if len(cs) != 0 {
			t.Errorf("expected 0 candidates for float64 package, got %d", len(cs))
		}
	}
}

func TestIntArithMutatesRem(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/rempkg"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	op := operators.IntArith{}
	var candidates []interface{}
	for _, f := range pkg.Files {
		cs := op.Find(f, pkg.TypesInfo)
		for _, c := range cs {
			candidates = append(candidates, c)
			if c.Original != "%" {
				t.Errorf("expected Original=%%, got %q", c.Original)
			}
			if c.Mutant != "*" {
				t.Errorf("expected Mutant=*, got %q", c.Mutant)
			}
		}
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
}

func TestIntArithSkipsConstantOnlyExpressions(t *testing.T) {
	// constpkg has "const x = 1 + 2" (both constant, skip) and "Add(a,b int)" (non-constant, find).
	pkg, err := source.Load(relDir(t, "testdata/constpkg"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	op := operators.IntArith{}
	var total int
	for _, f := range pkg.Files {
		total += len(op.Find(f, pkg.TypesInfo))
	}
	// Only the Add function's + should be found; the const 1+2 must be skipped.
	if total != 1 {
		t.Errorf("expected 1 candidate (Add's +), got %d", total)
	}
}
