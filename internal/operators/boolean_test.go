package operators

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/devenjarvis/cauldron/internal/mutation"
	"github.com/devenjarvis/cauldron/internal/schema"
	"github.com/devenjarvis/cauldron/internal/source"
)

func relDirBool(t *testing.T, sub string) string {
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

func TestBoolLogicFindsBothSwaps(t *testing.T) {
	pkg, err := source.Load(relDirBool(t, "testdata/boolpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, BoolLogic{}.Find(f, pkg.TypesInfo)...)
	}

	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %v", len(candidates), candidates)
	}

	found := map[string]bool{}
	for _, c := range candidates {
		found[c.Original+"→"+c.Mutant] = true
	}
	if !found["&&→||"] {
		t.Errorf("expected &&→|| candidate, got %v", candidates)
	}
	if !found["||→&&"] {
		t.Errorf("expected ||→&& candidate, got %v", candidates)
	}
}

func TestBoolLogicRewriteWrapsInClosures(t *testing.T) {
	pkg, err := source.Load(relDirBool(t, "testdata/boolpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{BoolLogic{}})
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for path, src := range rew.Files {
		count := strings.Count(src, "__cMutBool(")
		if count != 2 {
			t.Errorf("%s: expected 2 __cMutBool calls, got %d:\n%s", path, count, src)
		}
		if !strings.Contains(src, "func() bool") {
			t.Errorf("%s: expected func() bool closures:\n%s", path, src)
		}
		if !strings.Contains(src, "__cAnd") {
			t.Errorf("%s: expected __cAnd constant:\n%s", path, src)
		}
		if !strings.Contains(src, "__cOr") {
			t.Errorf("%s: expected __cOr constant:\n%s", path, src)
		}
	}
}
