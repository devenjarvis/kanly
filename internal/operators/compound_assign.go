package operators

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// IntCompoundAssign finds integer compound-assignment statements (arithmetic
// `+= -= *= /= %=` and bitwise/shift `&= |= ^= <<= >>= &^=`) and proposes
// the sibling-operator mutation matching IntArith and IntBitwise. It is the
// statement-level counterpart of those two operators.
//
// Type guard: both Lhs[0] and Rhs[0] must be integer-typed (underlying basic
// integer, plus shared defined type per intOperands).
//
// Rewrite: matches IncDec's closure-swap strategy. The original AssignStmt
// is replaced with __cMutStmt(func(){ x+=y }, func(){ x-=y }, mutIDs...) to
// avoid LHS re-evaluation on targets like map indices.
type IntCompoundAssign struct{}

func (IntCompoundAssign) Name() string { return "int_compound_assign" }

// DispatcherKey is shared with IncDec so the two operators emit a single
// __cMutStmt dispatcher, mirroring the IntCmpBoundary/IntCmpNegate pairing.
func (IntCompoundAssign) DispatcherKey() string { return "stmt_swap" }

// compoundSibling maps each int compound-assignment token to its mutation.
// Mirrors IntArith.sibling and IntBitwise.sibling at the assignment-token level.
var compoundSibling = map[token.Token]token.Token{
	token.ADD_ASSIGN:     token.SUB_ASSIGN,
	token.SUB_ASSIGN:     token.ADD_ASSIGN,
	token.MUL_ASSIGN:     token.QUO_ASSIGN,
	token.QUO_ASSIGN:     token.MUL_ASSIGN,
	token.REM_ASSIGN:     token.MUL_ASSIGN,
	token.AND_ASSIGN:     token.OR_ASSIGN,
	token.OR_ASSIGN:      token.AND_ASSIGN,
	token.XOR_ASSIGN:     token.AND_ASSIGN,
	token.SHL_ASSIGN:     token.SHR_ASSIGN,
	token.SHR_ASSIGN:     token.SHL_ASSIGN,
	token.AND_NOT_ASSIGN: token.AND_ASSIGN,
}

func (IntCompoundAssign) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate
	ast.Inspect(file, func(n ast.Node) bool {
		stmt, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if _, swap := compoundSibling[stmt.Tok]; !swap {
			return true
		}
		// Compound assignments are always single-LHS, single-RHS per the Go spec.
		if len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 {
			return true
		}
		if !intOperands(info, stmt.Lhs[0], stmt.Rhs[0]) {
			return true
		}
		candidates = append(candidates, mutation.Candidate{
			Node:     stmt,
			Pos:      stmt.TokPos,
			Original: stmt.Tok.String(),
			Mutant:   compoundSibling[stmt.Tok].String(),
		})
		return true
	})
	return candidates
}

// Rewrite replaces `x += y` with __cMutStmt(func(){ x += y }, func(){ x -= y }, mutIDs...).
func (IntCompoundAssign) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	stmt := c.Node.(*ast.AssignStmt)
	mutTok := compoundSibling[stmt.Tok]

	orig := &ast.AssignStmt{Lhs: stmt.Lhs, Tok: stmt.Tok, Rhs: stmt.Rhs}
	mut := &ast.AssignStmt{Lhs: stmt.Lhs, Tok: mutTok, Rhs: stmt.Rhs}

	return &ast.ExprStmt{X: stmtSwapCall(orig, mut, mutIDs)}
}
