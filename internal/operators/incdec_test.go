package operators_test

import (
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/operators"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

func TestIncDecFindsIntTargets(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/incdecpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	op := operators.IncDec{}
	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, op.Find(f, pkg.TypesInfo)...)
	}

	// Expected positives: IdentInc, IdentDec, IndexedInc, SelectorInc, ForLoopInc = 5.
	// Negatives: FloatInc, Int32Inc.
	if len(candidates) != 5 {
		t.Fatalf("expected 5 candidates, got %d", len(candidates))
	}

	var incs, decs int
	for _, c := range candidates {
		switch c.Original {
		case "++":
			if c.Mutant != "--" {
				t.Errorf("++ mutant: want --, got %q", c.Mutant)
			}
			incs++
		case "--":
			if c.Mutant != "++" {
				t.Errorf("-- mutant: want ++, got %q", c.Mutant)
			}
			decs++
		default:
			t.Errorf("unexpected Original %q", c.Original)
		}
	}
	if incs != 4 || decs != 1 {
		t.Errorf("counts: want 4 ++ and 1 --, got %d ++ and %d --", incs, decs)
	}
}

func TestIncDecSkipsNamedInt(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/namedincdecpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	op := operators.IncDec{}
	var total int
	for _, f := range pkg.Files {
		total += len(op.Find(f, pkg.TypesInfo))
	}
	if total != 0 {
		t.Errorf("expected 0 candidates for named-int package, got %d", total)
	}
}

func TestIncDecRewriteEmitsStmtSwap(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/incdecpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{operators.IncDec{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for path, src := range rew.Files {
		count := strings.Count(src, "__cMutStmt(")
		if count != 5 {
			t.Errorf("%s: expected 5 __cMutStmt calls, got %d:\n%s", path, count, src)
		}
		// The mutant closure must contain the flipped token.
		if !strings.Contains(src, "x--") {
			t.Errorf("%s: expected flipped x-- in mutant closure", path)
		}
		if !strings.Contains(src, "arr[i]++") {
			t.Errorf("%s: expected original arr[i]++ preserved in closure", path)
		}
	}

	// Dispatcher must include __cMutStmt.
	if !strings.Contains(rew.Dispatcher, "func __cMutStmt(orig, mut func()") {
		t.Errorf("dispatcher missing __cMutStmt definition:\n%s", rew.Dispatcher)
	}
}
