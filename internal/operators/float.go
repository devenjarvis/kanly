package operators

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// FloatArith is IntArith for floating-point operands: `+↔-` and `*↔/` on
// float32, float64 and named wrappers around them. There is no `%` on floats.
type FloatArith struct{}

func (FloatArith) Name() string          { return "float_arith" }
func (FloatArith) DispatcherKey() string { return "float_arith" }

var floatSibling = map[token.Token]token.Token{
	token.ADD: token.SUB,
	token.SUB: token.ADD,
	token.MUL: token.QUO,
	token.QUO: token.MUL,
}

func (FloatArith) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate
	ast.Inspect(file, func(n ast.Node) bool {
		expr, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		mutOp, ok := floatSibling[expr.Op]
		if !ok || !floatOperands(info, expr.X, expr.Y) {
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

// Rewrite returns __cMutFloat(x, y, opcode, mutIDs...).
func (FloatArith) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	expr := c.Node.(*ast.BinaryExpr)
	return opCall("__cMutFloat", expr.X, expr.Y, opcodeConst[expr.Op], mutIDs)
}

// FloatCmpBoundary is IntCmpBoundary for floating-point operands: `<↔<=`
// and `>↔>=`.
type FloatCmpBoundary struct{}

func (FloatCmpBoundary) Name() string          { return "float_cmp_boundary" }
func (FloatCmpBoundary) DispatcherKey() string { return "float_cmp" }

func (FloatCmpBoundary) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	return findFloatCmp(file, info, boundarySibling)
}

func (FloatCmpBoundary) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	expr := c.Node.(*ast.BinaryExpr)
	return opCall("__cMutFloatCmp", expr.X, expr.Y, cmpOpcodeConst[expr.Op], mutIDs)
}

// FloatCmpNegate is IntCmpNegate for floating-point operands: each of the
// six comparisons becomes its negation. With a NaN operand `a < b` and
// `a >= b` are both false, so the mutant is not the logical complement
// there; it is still the plausible slip it models.
type FloatCmpNegate struct{}

func (FloatCmpNegate) Name() string          { return "float_cmp_negate" }
func (FloatCmpNegate) DispatcherKey() string { return "float_cmp" }

func (FloatCmpNegate) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	return findFloatCmp(file, info, negateMap)
}

func (FloatCmpNegate) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	expr := c.Node.(*ast.BinaryExpr)
	return opCall("__cMutFloatCmp", expr.X, expr.Y, cmpOpcodeConst[expr.Op], mutIDs)
}

func findFloatCmp(file *ast.File, info *types.Info, mutants map[token.Token]token.Token) []mutation.Candidate {
	var candidates []mutation.Candidate
	ast.Inspect(file, func(n ast.Node) bool {
		expr, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		mutOp, ok := mutants[expr.Op]
		if !ok {
			return true
		}
		// __cMutFloatCmp returns plain bool, which does not fit a
		// named-bool context.
		if !floatOperands(info, expr.X, expr.Y) || !isPlainBoolResult(info, expr) {
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

// opCall builds fn(x, y, opcode, mutIDs...), the call shape of the binary
// operator dispatchers.
func opCall(fn string, x, y ast.Expr, opcode string, mutIDs []int) *ast.CallExpr {
	return idCall(fn, []ast.Expr{x, y, ast.NewIdent(opcode)}, mutIDs)
}
