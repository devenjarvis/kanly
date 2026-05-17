package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// boundarySibling maps each boundary-swappable comparison op to its counterpart.
var boundarySibling = map[token.Token]token.Token{
	token.LSS: token.LEQ,
	token.LEQ: token.LSS,
	token.GTR: token.GEQ,
	token.GEQ: token.GTR,
}

// negateMap maps each int comparison op to its logical negation.
var negateMap = map[token.Token]token.Token{
	token.LSS: token.GEQ,
	token.GTR: token.LEQ,
	token.LEQ: token.GTR,
	token.GEQ: token.LSS,
	token.EQL: token.NEQ,
	token.NEQ: token.EQL,
}

// cmpOpcodeConst maps each comparison op to the dispatcher constant name.
var cmpOpcodeConst = map[token.Token]string{
	token.LSS: "__cLT",
	token.LEQ: "__cLE",
	token.GTR: "__cGT",
	token.GEQ: "__cGE",
	token.EQL: "__cEQ",
	token.NEQ: "__cNE",
}

// emitIntCmpCall builds a __cMutIntCmp call for the given binary expression and mutIDs.
func emitIntCmpCall(expr *ast.BinaryExpr, mutIDs []int) ast.Node {
	opcode, ok := cmpOpcodeConst[expr.Op]
	if !ok {
		opcode = "__cLT"
	}
	args := []ast.Expr{expr.X, expr.Y, &ast.Ident{Name: opcode}}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	return &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutIntCmp"}, Args: args}
}

// IntCmpBoundary mutates boundary comparisons: < ↔ <= and > ↔ >= on plain int operands.
// == and != are excluded (they have no boundary neighbour).
type IntCmpBoundary struct{}

func (IntCmpBoundary) Name() string          { return "int_cmp_boundary" }
func (IntCmpBoundary) DispatcherKey() string { return "int_cmp" }

func (IntCmpBoundary) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate
	ast.Inspect(file, func(n ast.Node) bool {
		expr, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		mutOp, ok := boundarySibling[expr.Op]
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

func (IntCmpBoundary) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	return emitIntCmpCall(c.Node.(*ast.BinaryExpr), mutIDs)
}

// IntCmpNegate mutates every int comparison to its logical negation:
// < → >=, > → <=, <= → >, >= → <, == → !=, != → ==.
type IntCmpNegate struct{}

func (IntCmpNegate) Name() string          { return "int_cmp_negate" }
func (IntCmpNegate) DispatcherKey() string { return "int_cmp" }

func (IntCmpNegate) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate
	ast.Inspect(file, func(n ast.Node) bool {
		expr, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		mutOp, ok := negateMap[expr.Op]
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

func (IntCmpNegate) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	return emitIntCmpCall(c.Node.(*ast.BinaryExpr), mutIDs)
}
