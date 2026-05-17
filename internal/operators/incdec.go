package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// IncDec finds *ast.IncDecStmt on int-typed targets and proposes swapping
// the increment/decrement direction (`x++` ↔ `x--`). Idiomatic Go loops
// and counters consist almost entirely of these statements; without this
// operator they are silently unmutated.
//
// Type guard: the target expression's static type must be exactly types.Int
// (no named-int variants, no sized ints), matching the strict identity
// policy used by IntArith.
//
// Rewrite: the original IncDecStmt is replaced with an ExprStmt-wrapped
// call to __cMutStmt(orig, mut, mutIDs...) where orig and mut are no-arg
// closures running the original and flipped statement respectively. The
// closure form avoids re-evaluating the target expression — critical for
// targets like m["k"]++ where inline rewriting would evaluate the LHS twice.
type IncDec struct{}

func (IncDec) Name() string          { return "inc_dec" }
func (IncDec) DispatcherKey() string { return "stmt_swap" }

// incDecSibling maps each token to its swap target.
var incDecSibling = map[token.Token]token.Token{
	token.INC: token.DEC,
	token.DEC: token.INC,
}

func (IncDec) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate
	ast.Inspect(file, func(n ast.Node) bool {
		stmt, ok := n.(*ast.IncDecStmt)
		if !ok {
			return true
		}
		if !intOperand(info, stmt.X) {
			return true
		}
		candidates = append(candidates, mutation.Candidate{
			Node:     stmt,
			Pos:      stmt.TokPos,
			Original: stmt.Tok.String(),
			Mutant:   incDecSibling[stmt.Tok].String(),
		})
		return true
	})
	return candidates
}

// Rewrite replaces `x++` with __cMutStmt(func() { x++ }, func() { x-- }, mutIDs...).
func (IncDec) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	stmt := c.Node.(*ast.IncDecStmt)
	mutTok := incDecSibling[stmt.Tok]

	origBody := &ast.IncDecStmt{X: stmt.X, Tok: stmt.Tok}
	mutBody := &ast.IncDecStmt{X: stmt.X, Tok: mutTok}

	return &ast.ExprStmt{X: stmtSwapCall(origBody, mutBody, mutIDs)}
}

// stmtSwapCall builds the __cMutStmt(func(){orig}, func(){mut}, mutIDs...) call expression.
// Shared between IncDec and IntCompoundAssign — both replace a statement with a
// pair of closure-wrapped statements.
func stmtSwapCall(orig, mut ast.Stmt, mutIDs []int) *ast.CallExpr {
	args := []ast.Expr{stmtFuncLit(orig), stmtFuncLit(mut)}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	return &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutStmt"}, Args: args}
}

// stmtFuncLit wraps a single statement in func() { stmt }.
func stmtFuncLit(stmt ast.Stmt) *ast.FuncLit {
	return &ast.FuncLit{
		Type: &ast.FuncType{Params: &ast.FieldList{}},
		Body: &ast.BlockStmt{List: []ast.Stmt{stmt}},
	}
}
