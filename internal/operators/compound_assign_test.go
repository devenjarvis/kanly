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

	// Arithmetic positives: AddAssign, SubAssign, MulAssign, QuoAssign, RemAssign,
	// IndexedAssign (all plain int) plus Int32Compound (sized int) = 7.
	// Bitwise positives: BitwiseAnd, BitwiseOr, BitwiseXor, Shl, Shr, AndNot = 6.
	// Float is still rejected. Total = 13.
	if len(candidates) != 13 {
		t.Fatalf("expected 13 candidates, got %d", len(candidates))
	}

	wantMutant := map[string]string{
		"+=":  "-=",
		"-=":  "+=",
		"*=":  "/=",
		"/=":  "*=",
		"%=":  "*=",
		"&=":  "|=",
		"|=":  "&=",
		"^=":  "&=",
		"<<=": ">>=",
		">>=": "<<=",
		"&^=": "&=",
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
	// AddAssign, IndexedAssign, and Int32Compound all produce += candidates.
	if seen["+="] != 3 {
		t.Errorf("+= count: want 3 (AddAssign + IndexedAssign + Int32Compound), got %d", seen["+="])
	}
	for _, op := range []string{"-=", "*=", "/=", "%=", "&=", "|=", "^=", "<<=", ">>=", "&^="} {
		if seen[op] != 1 {
			t.Errorf("%s count: want 1, got %d", op, seen[op])
		}
	}
}

func TestIntCompoundAssignAcceptsNamedInt(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/namedcompoundpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	op := operators.IntCompoundAssign{}
	var total int
	for _, f := range pkg.Files {
		total += len(op.Find(f, pkg.TypesInfo))
	}
	// NamedAddAssign on MyInt — mutated under the underlying-integer policy.
	if total != 1 {
		t.Errorf("expected 1 candidate for named-int compound-assign package, got %d", total)
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
		if count != 13 {
			t.Errorf("%s: expected 13 __cMutStmt calls, got %d:\n%s", path, count, src)
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
