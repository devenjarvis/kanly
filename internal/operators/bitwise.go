package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// IntBitwise finds integer bitwise/shift binary expressions and proposes
// canonical sibling mutations matching IntArith's per-op single-mutant policy.
// Supported ops: & ↔ |, ^ → &, << ↔ >>, &^ → &.
type IntBitwise struct{}

func (IntBitwise) Name() string          { return "int_bitwise" }
func (IntBitwise) DispatcherKey() string { return "int_bitwise" }

var bitwiseSibling = map[token.Token]token.Token{
	token.AND:     token.OR,
	token.OR:      token.AND,
	token.XOR:     token.AND,
	token.SHL:     token.SHR,
	token.SHR:     token.SHL,
	token.AND_NOT: token.AND,
}

var bitwiseOpcodeConst = map[token.Token]string{
	token.AND:     "__cAnd2",
	token.OR:      "__cOr2",
	token.XOR:     "__cXor",
	token.SHL:     "__cShl",
	token.SHR:     "__cShr",
	token.AND_NOT: "__cAndNot",
}

func (IntBitwise) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate
	ast.Inspect(file, func(n ast.Node) bool {
		expr, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		mutOp, ok := bitwiseSibling[expr.Op]
		if !ok {
			return true
		}
		if !intOperands(info, expr.X, expr.Y) {
			return true
		}
		candidates = append(candidates, mutation.Candidate{
			Node:     expr,
			Pos:      expr.OpPos,
			Original: expr.Op.String(),
			Mutant:   mutOp.String(),
		})
		return true
	})
	return candidates
}

// Rewrite returns the __cMutIntBit dispatcher call that replaces the original BinaryExpr.
func (IntBitwise) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	expr := c.Node.(*ast.BinaryExpr)
	opcode := bitwiseOpcodeConst[expr.Op]
	args := []ast.Expr{expr.X, expr.Y, &ast.Ident{Name: opcode}}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	return &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutIntBit"}, Args: args}
}
