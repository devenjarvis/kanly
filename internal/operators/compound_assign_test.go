package operators_test

import (
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/operators"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

func TestIntCompoundAssignFindsAllArithmeticTokens(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/compoundpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	op := operators.IntCompoundAssign{}
	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, op.Find(f, pkg.TypesInfo)...)
	}

	// Positives: AddAssign, SubAssign, MulAssign, QuoAssign, RemAssign, IndexedAssign = 6.
	if len(candidates) != 6 {
		t.Fatalf("expected 6 candidates, got %d", len(candidates))
	}

	wantMutant := map[string]string{
		"+=": "-=",
		"-=": "+=",
		"*=": "/=",
		"/=": "*=",
		"%=": "*=",
	}
	seen := make(map[string]int)
	for _, c := range candidates {
		want, ok := wantMutant[c.Original]
		if !ok {
			t.Errorf("unexpected Original %q", c.Original)
			continue
		}
		if c.Mutant != want {
			t.Errorf("%s mutant: want %q, got %q", c.Original, want, c.Mutant)
		}
		seen[c.Original]++
	}
	// AddAssign function and IndexedAssign both produce += candidates.
	if seen["+="] != 2 {
		t.Errorf("+= count: want 2 (AddAssign + IndexedAssign), got %d", seen["+="])
	}
	for _, op := range []string{"-=", "*=", "/=", "%="} {
		if seen[op] != 1 {
			t.Errorf("%s count: want 1, got %d", op, seen[op])
		}
	}
}

func TestIntCompoundAssignSkipsNonIntAndBitwise(t *testing.T) {
	// compoundpkg.go contains float, int32, and bitwise compound-assign
	// negative cases — they must all be rejected.
	pkg, err := source.Load(relDir(t, "testdata/compoundpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	op := operators.IntCompoundAssign{}
	for _, f := range pkg.Files {
		for _, c := range op.Find(f, pkg.TypesInfo) {
			// Make sure no bitwise / shift token slipped through.
			switch c.Original {
			case "&=", "|=", "^=", "<<=", ">>=", "&^=":
				t.Errorf("bitwise compound op %q must not be a candidate", c.Original)
			}
		}
	}
}

func TestIntCompoundAssignSkipsNamedInt(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/namedcompoundpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	op := operators.IntCompoundAssign{}
	var total int
	for _, f := range pkg.Files {
		total += len(op.Find(f, pkg.TypesInfo))
	}
	if total != 0 {
		t.Errorf("expected 0 candidates for named-int compound-assign package, got %d", total)
	}
}

func TestIntCompoundAssignRewriteEmitsStmtSwap(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/compoundpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{operators.IntCompoundAssign{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for path, src := range rew.Files {
		count := strings.Count(src, "__cMutStmt(")
		if count != 6 {
			t.Errorf("%s: expected 6 __cMutStmt calls, got %d:\n%s", path, count, src)
		}
		// Indexed LHS must remain inside the closures — proving single-eval safety.
		if !strings.Contains(src, "m[key] += delta") {
			t.Errorf("%s: expected `m[key] += delta` preserved inside closure", path)
		}
		if !strings.Contains(src, "m[key] -= delta") {
			t.Errorf("%s: expected mutant `m[key] -= delta` inside closure", path)
		}
	}
}

func TestIncDecAndCompoundShareDispatcher(t *testing.T) {
	// Both operators emit DispatcherKey "stmt_swap" and share a single
	// __cMutStmt dispatcher in the generated code.
	pkg, err := source.Load(relDir(t, "testdata/compoundpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{
		operators.IncDec{},
		operators.IntCompoundAssign{},
	}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	// Exactly one __cMutStmt function definition.
	defCount := strings.Count(rew.Dispatcher, "func __cMutStmt(")
	if defCount != 1 {
		t.Errorf("expected exactly 1 __cMutStmt definition, got %d:\n%s", defCount, rew.Dispatcher)
	}
}
