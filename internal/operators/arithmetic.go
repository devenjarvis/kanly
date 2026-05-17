package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/cauldron/internal/mutation"
)

// IntArith finds integer binary arithmetic expressions and proposes canonical sibling mutations.
type IntArith struct{}

func (IntArith) Name() string { return "int_arith" }

// sibling maps each arithmetic op to its canonical mutation counterpart.
var sibling = map[token.Token]token.Token{
	token.ADD: token.SUB,
	token.SUB: token.ADD,
	token.MUL: token.QUO,
	token.QUO: token.MUL,
}

// opcodeConst maps each arithmetic op to the opcode constant name used in the dispatcher.
var opcodeConst = map[token.Token]string{
	token.ADD: "__cAdd",
	token.SUB: "__cSub",
	token.MUL: "__cMul",
	token.QUO: "__cQuo",
}

// Rewrite returns the __cMutInt dispatcher call that replaces the original BinaryExpr.
func (IntArith) Rewrite(c mutation.Candidate, mutID int) ast.Node {
	expr := c.Node.(*ast.BinaryExpr)
	opcode := opcodeConst[expr.Op]
	return &ast.CallExpr{
		Fun: &ast.Ident{Name: "__cMutInt"},
		Args: []ast.Expr{
			expr.X,
			expr.Y,
			&ast.Ident{Name: opcode},
			&ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", mutID)},
		},
	}
}

func (IntArith) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate

	ast.Inspect(file, func(n ast.Node) bool {
		expr, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}

		mutOp, ok := sibling[expr.Op]
		if !ok {
			return true
		}

		// Skip if both operands are untyped constants (compile-time folded, no runtime effect).
		lv, lok := info.Types[expr.X]
		rv, rok := info.Types[expr.Y]
		if !lok || !rok {
			return true
		}
		if lv.Value != nil && rv.Value != nil {
			return true
		}

		// Only mutate when both operands are exactly types.Int (not int32, int64, named-int types).
		// Do NOT use .Underlying() — named types like "type MyInt int" must be excluded.
		if lv.Type != types.Typ[types.Int] || rv.Type != types.Typ[types.Int] {
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

