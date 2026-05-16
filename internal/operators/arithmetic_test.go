package operators_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/devenjarvis/cauldron/internal/operators"
	"github.com/devenjarvis/cauldron/internal/source"
)

func testdataDir(t *testing.T, sub string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", sub)
}

func TestIntArithFindsAddOnly(t *testing.T) {
	pkg, err := source.Load(testdataDir(t, "../../source/testdata/simple"))
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
	pkg, err := source.Load(testdataDir(t, "floatpkg"))
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
