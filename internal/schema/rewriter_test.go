package schema_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/operators"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
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

func TestRewriteReplacesPlusWithDispatcherCall(t *testing.T) {
	pkg, err := source.Load(relDir(t, "../source/testdata/simple"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{operators.IntArith{}}, nil)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	if len(rew.Mutations) != 1 {
		t.Fatalf("expected 1 mutation, got %d", len(rew.Mutations))
	}
	if rew.Mutations[0].ID != 1 {
		t.Errorf("expected mutation ID 1, got %d", rew.Mutations[0].ID)
	}

	// Exactly one rewritten source file should be present.
	if len(rew.Files) != 1 {
		t.Fatalf("expected 1 rewritten file, got %d", len(rew.Files))
	}

	for _, content := range rew.Files {
		if !strings.Contains(content, "__cMutInt(") {
			t.Errorf("rewritten file does not contain __cMutInt call:\n%s", content)
		}
		if strings.Contains(content, "a + b") {
			t.Errorf("rewritten file still contains bare a + b:\n%s", content)
		}
	}
}

// doubleArithOp returns two candidates for the same *ast.BinaryExpr node, letting us verify
// that the rewriter aggregates them into a single dispatcher call with two mutIDs.
type doubleArithOp struct{}

func (doubleArithOp) Name() string          { return "double_arith" }
func (doubleArithOp) DispatcherKey() string { return "int_arith" }

func (doubleArithOp) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var result []mutation.Candidate
	ast.Inspect(file, func(n ast.Node) bool {
		expr, ok := n.(*ast.BinaryExpr)
		if !ok || expr.Op != token.ADD {
			return true
		}
		// Return the same node twice with different mutants.
		result = append(result,
			mutation.Candidate{Node: expr, Pos: expr.OpPos, Original: "+", Mutant: "-"},
			mutation.Candidate{Node: expr, Pos: expr.OpPos, Original: "+", Mutant: "*"},
		)
		return true
	})
	return result
}

func (doubleArithOp) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	expr := c.Node.(*ast.BinaryExpr)
	args := []ast.Expr{expr.X, expr.Y, &ast.Ident{Name: "__cAdd"}}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	return &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutInt"}, Args: args}
}

func TestRewriteSetsPackageOnMutations(t *testing.T) {
	pkg, err := source.Load(relDir(t, "../runner/testdata/sample"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{operators.IntArith{}}, nil)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	if len(rew.Mutations) == 0 {
		t.Fatal("expected at least one mutation")
	}
	wantPkg := "github.com/devenjarvis/kanly/internal/runner/testdata/sample"
	for i, m := range rew.Mutations {
		if m.Package != wantPkg {
			t.Errorf("Mutations[%d].Package: want %q, got %q", i, wantPkg, m.Package)
		}
	}
}

func TestRewriteAggregatesMutIDsPerNode(t *testing.T) {
	pkg, err := source.Load(relDir(t, "../source/testdata/simple"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{doubleArithOp{}}, nil)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	if len(rew.Mutations) != 2 {
		t.Fatalf("expected 2 mutations, got %d", len(rew.Mutations))
	}
	if len(rew.Files) != 1 {
		t.Fatalf("expected 1 rewritten file, got %d", len(rew.Files))
	}

	for _, content := range rew.Files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "rewritten.go", content, 0)
		if err != nil {
			t.Fatalf("parse rewritten source: %v\n%s", err, content)
		}

		var calls []*ast.CallExpr
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if ident, ok := call.Fun.(*ast.Ident); ok && strings.HasPrefix(ident.Name, "__cMut") {
				calls = append(calls, call)
			}
			return true
		})

		if len(calls) != 1 {
			t.Fatalf("expected exactly one __cMut* call, got %d:\n%s", len(calls), content)
		}

		// Args: a, b, opcode, mutID1, mutID2 → 5 total
		call := calls[0]
		if len(call.Args) != 5 {
			t.Fatalf("expected 5 args (a, b, op, mutID1, mutID2), got %d:\n%s", len(call.Args), content)
		}
		for i, wantID := range []int{1, 2} {
			lit, ok := call.Args[3+i].(*ast.BasicLit)
			if !ok {
				t.Errorf("arg[%d] is not BasicLit: %T", 3+i, call.Args[3+i])
				continue
			}
			if lit.Value != fmt.Sprintf("%d", wantID) {
				t.Errorf("arg[%d]: want %d, got %s", 3+i, wantID, lit.Value)
			}
		}
	}
}

// TestRewriteFunctionFilter exercises the widened (file, line, funcName)
// predicate: only mutations inside the named function are emitted.
func TestRewriteFunctionFilter(t *testing.T) {
	pkg, err := source.Load(relDir(t, "../runner/testdata/sample"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	keepAdd := func(_ string, _ int, funcName string) bool {
		return funcName == "Add"
	}
	rew, err := schema.Rewrite(pkg, []mutation.Operator{operators.IntArith{}}, keepAdd)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if len(rew.Mutations) != 1 {
		t.Fatalf("expected 1 mutation (Add only), got %d", len(rew.Mutations))
	}
	if got := rew.Mutations[0].Function; got != "Add" {
		t.Errorf("Function: want Add, got %q", got)
	}
}

func TestFuncNames(t *testing.T) {
	pkg, err := source.Load(relDir(t, "../runner/testdata/sample"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	names := schema.FuncNames(pkg)
	want := map[string]bool{"Add": true, "Sub": true}
	if len(names) != len(want) {
		t.Fatalf("FuncNames: want %d entries, got %d (%v)", len(want), len(names), names)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected name %q", n)
		}
	}
}

// TestRewriteCallDeleteAndIntArithCoexist verifies that two operators with
// different DispatcherKeys can target distinct nodes in the same file without
// stomping on each other in the rewriter's node→group flattening.
func TestRewriteCallDeleteAndIntArithCoexist(t *testing.T) {
	pkg, err := source.Load(relDir(t, "../operators/testdata/calldelete"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{operators.IntArith{}, operators.CallDelete{}}, nil)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	var sawArith, sawCallDelete bool
	for _, m := range rew.Mutations {
		switch m.OperatorName {
		case "int_arith":
			sawArith = true
		case "call_delete":
			sawCallDelete = true
		}
	}
	if !sawArith {
		t.Errorf("expected at least one int_arith mutation (MixedOps `a + b`), got none")
	}
	if !sawCallDelete {
		t.Errorf("expected at least one call_delete mutation, got none")
	}

	for path, src := range rew.Files {
		if !strings.Contains(src, "__cMutInt(") {
			t.Errorf("%s: expected __cMutInt( in rewritten source:\n%s", path, src)
		}
		if !strings.Contains(src, "__cMutCallSkip(") {
			t.Errorf("%s: expected __cMutCallSkip( in rewritten source:\n%s", path, src)
		}
	}
	if !strings.Contains(rew.Dispatcher, "__cMutInt") {
		t.Errorf("expected __cMutInt in dispatcher")
	}
	if !strings.Contains(rew.Dispatcher, "__cMutCallSkip") {
		t.Errorf("expected __cMutCallSkip in dispatcher")
	}
}
