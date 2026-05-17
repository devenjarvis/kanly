package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// CallDelete finds void-returning call statements (an *ast.ExprStmt wrapping an
// *ast.CallExpr) and proposes deleting the entire call. Under an active mutant
// neither the call nor its argument expressions are evaluated — lazy semantics
// matching PIT's "Void Method Call Removal" mutator.
//
// Exclusions: defer/go-wrapped calls are *ast.DeferStmt / *ast.GoStmt, not
// *ast.ExprStmt, and are naturally skipped. The builtins panic, print, and
// println are excluded by Find — deleting panic gives a low-signal flip
// (crash-or-not-crash); print/println are debug-only. close, delete, clear,
// and recover are kept: they have observable semantics worth catching.
//
// Rewriter interaction: this operator targets *ast.CallExpr nodes whose parent
// is an *ast.ExprStmt. The schema rewriter's node→group flattening
// (internal/schema/rewriter.go:146-149) silently drops collisions where two
// operators target the same node with different DispatcherKeys. No current
// operator collides; future operators targeting CallExpr-under-ExprStmt must
// coordinate.
type CallDelete struct{}

func (CallDelete) Name() string          { return "call_delete" }
func (CallDelete) DispatcherKey() string { return "call_delete" }

func (CallDelete) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate
	ast.Inspect(file, func(n ast.Node) bool {
		stmt, ok := n.(*ast.ExprStmt)
		if !ok {
			return true
		}
		call, ok := stmt.X.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isExcludedBuiltin(info, call.Fun) {
			return true
		}
		candidates = append(candidates, mutation.Candidate{
			Node:     call,
			Pos:      call.Pos(),
			Original: calleeName(call) + "(...)",
			Mutant:   "",
		})
		return true
	})
	return candidates
}

// Rewrite replaces the original call with __cMutCallSkip(func(){ origCall() }, mutIDs...).
// The closure defers evaluation of both the call and its args until the dispatcher
// decides whether to invoke it.
func (CallDelete) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	call := c.Node.(*ast.CallExpr)
	args := []ast.Expr{voidFuncLit(call)}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	return &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutCallSkip"}, Args: args}
}

// calleeName renders the call target as a best-effort string for the report.
// Chained selectors (a.b.c()) and unusual forms fall back to the leaf name or "call".
func calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok {
			return x.Name + "." + fn.Sel.Name
		}
		return fn.Sel.Name
	case *ast.IndexExpr:
		if id, ok := fn.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.IndexListExpr:
		if id, ok := fn.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return "call"
}

// voidFuncLit wraps a CallExpr in func() { origCall() } — a no-arg, no-result closure.
func voidFuncLit(call *ast.CallExpr) *ast.FuncLit {
	return &ast.FuncLit{
		Type: &ast.FuncType{Params: &ast.FieldList{}},
		Body: &ast.BlockStmt{
			List: []ast.Stmt{&ast.ExprStmt{X: call}},
		},
	}
}

// isExcludedBuiltin reports whether fn resolves to a builtin we don't mutate.
// Uses types.Info.Uses so a user-defined function shadowing panic/print/println
// is NOT excluded (mirrors the strict-identity policy in typecheck.go).
func isExcludedBuiltin(info *types.Info, fn ast.Expr) bool {
	ident, ok := fn.(*ast.Ident)
	if !ok {
		return false
	}
	builtin, ok := info.Uses[ident].(*types.Builtin)
	if !ok {
		return false
	}
	switch builtin.Name() {
	case "panic", "print", "println":
		return true
	}
	return false
}
