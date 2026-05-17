package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/cauldron/internal/mutation"
)

// BoolLogic finds boolean binary expressions and swaps && ↔ ||.
// Short-circuit semantics are preserved by wrapping each operand in a closure.
type BoolLogic struct{}

func (BoolLogic) Name() string          { return "bool_logic" }
func (BoolLogic) DispatcherKey() string { return "bool_logic" }

func (BoolLogic) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate
	ast.Inspect(file, func(n ast.Node) bool {
		expr, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		if expr.Op != token.LAND && expr.Op != token.LOR {
			return true
		}
		if !boolOperands(info, expr.X, expr.Y) {
			return true
		}
		var mutOp token.Token
		if expr.Op == token.LAND {
			mutOp = token.LOR
		} else {
			mutOp = token.LAND
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

// Rewrite wraps each operand in a func() bool closure to preserve short-circuit semantics.
func (BoolLogic) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	expr := c.Node.(*ast.BinaryExpr)
	var opcode string
	if expr.Op == token.LAND {
		opcode = "__cAnd"
	} else {
		opcode = "__cOr"
	}
	args := []ast.Expr{
		boolFuncLit(expr.X),
		boolFuncLit(expr.Y),
		&ast.Ident{Name: opcode},
	}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	return &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutBool"}, Args: args}
}

// boolFuncLit wraps expr in func() bool { return expr } preserving the original node pointer
// so astutil.Apply can recurse into nested bool expressions and rewrite them too.
func boolFuncLit(expr ast.Expr) *ast.FuncLit {
	return &ast.FuncLit{
		Type: &ast.FuncType{
			Results: &ast.FieldList{
				List: []*ast.Field{
					{Type: &ast.Ident{Name: "bool"}},
				},
			},
		},
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.ReturnStmt{Results: []ast.Expr{expr}},
			},
		},
	}
}

