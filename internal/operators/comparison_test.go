package operators_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/operators"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

func cmpPkg(t *testing.T) *source.Package {
	t.Helper()
	pkg, err := source.Load(relDir(t, "testdata/cmppkg"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return pkg
}

func TestIntCmpBoundaryFindsFourSwaps(t *testing.T) {
	pkg := cmpPkg(t)

	op := operators.IntCmpBoundary{}
	var candidates []struct{ orig, mutant string }
	for _, f := range pkg.Files {
		for _, c := range op.Find(f, pkg.TypesInfo) {
			candidates = append(candidates, struct{ orig, mutant string }{c.Original, c.Mutant})
		}
	}

	if len(candidates) != 4 {
		t.Fatalf("expected 4 candidates, got %d: %v", len(candidates), candidates)
	}

	want := map[string]string{
		"<":  "<=",
		"<=": "<",
		">":  ">=",
		">=": ">",
	}
	for _, c := range candidates {
		exp, ok := want[c.orig]
		if !ok {
			t.Errorf("unexpected original op %q", c.orig)
			continue
		}
		if c.mutant != exp {
			t.Errorf("op %q: expected mutant %q, got %q", c.orig, exp, c.mutant)
		}
	}
}

func TestIntCmpNegateFindsAllSix(t *testing.T) {
	pkg := cmpPkg(t)

	op := operators.IntCmpNegate{}
	var candidates []struct{ orig, mutant string }
	for _, f := range pkg.Files {
		for _, c := range op.Find(f, pkg.TypesInfo) {
			candidates = append(candidates, struct{ orig, mutant string }{c.Original, c.Mutant})
		}
	}

	if len(candidates) != 6 {
		t.Fatalf("expected 6 candidates, got %d: %v", len(candidates), candidates)
	}

	want := map[string]string{
		"<":  ">=",
		">":  "<=",
		"<=": ">",
		">=": "<",
		"==": "!=",
		"!=": "==",
	}
	for _, c := range candidates {
		exp, ok := want[c.orig]
		if !ok {
			t.Errorf("unexpected original op %q", c.orig)
			continue
		}
		if c.mutant != exp {
			t.Errorf("op %q: expected mutant %q, got %q", c.orig, exp, c.mutant)
		}
	}
}

func TestIntCmpCoLocatedAggregates(t *testing.T) {
	pkg := cmpPkg(t)

	ops := []mutation.Operator{operators.IntCmpBoundary{}, operators.IntCmpNegate{}}
	rew, err := schema.Rewrite(pkg, ops)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	// 4 boundary + 6 negate = 10 total mutations
	if len(rew.Mutations) != 10 {
		t.Fatalf("expected 10 mutations, got %d", len(rew.Mutations))
	}

	// Find the rewritten file containing Lt (a < b).
	var ltSrc string
	for _, content := range rew.Files {
		if strings.Contains(content, "func Lt(") {
			ltSrc = content
			break
		}
	}
	if ltSrc == "" {
		t.Fatal("could not find rewritten file containing Lt")
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "lt.go", ltSrc, 0)
	if err != nil {
		t.Fatalf("parse rewritten source: %v\n%s", err, ltSrc)
	}

	// Walk only the Lt function body to count __cMutIntCmp calls.
	var ltFunc *ast.FuncDecl
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "Lt" {
			ltFunc = fn
			break
		}
	}
	if ltFunc == nil {
		t.Fatal("Lt function not found in rewritten source")
	}

	// The '<' in Lt is matched by both IntCmpBoundary and IntCmpNegate:
	// the rewriter must produce exactly one __cMutIntCmp call carrying both mutIDs.
	var calls []*ast.CallExpr
	ast.Inspect(ltFunc.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "__cMutIntCmp" {
			calls = append(calls, call)
		}
		return true
	})

	if len(calls) != 1 {
		t.Fatalf("Lt: expected exactly 1 __cMutIntCmp call, got %d:\n%s", len(calls), ltSrc)
	}

	// Args: a, b, opcode, mutID1, mutID2 → 5 total (one boundary + one negate mutID)
	call := calls[0]
	if len(call.Args) != 5 {
		t.Errorf("Lt call: expected 5 args (a, b, op, id1, id2), got %d:\n%s", len(call.Args), ltSrc)
	}
	// Last two args must be BasicLit integers
	for i := 3; i < len(call.Args); i++ {
		if _, ok := call.Args[i].(*ast.BasicLit); !ok {
			t.Errorf("Lt call arg[%d] is not BasicLit: %T", i, call.Args[i])
		}
	}
}
