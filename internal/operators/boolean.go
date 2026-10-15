package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// BoolLogic finds boolean binary expressions and swaps && ↔ ||.
// Short-circuit semantics are preserved by wrapping the right operand in a
// closure; the left operand is always evaluated first, so it is passed as a
// plain argument and fixes the dispatcher's result type.
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
		if !boolOperands(info, expr.X, expr.Y) || !boolResultInferable(info, expr, expr.X) {
			return true
		}
		if containsRecover(info, expr.Y) {
			// recover() only works when called directly by a deferred
			// function; inside the closure it would always return nil.
			return true
		}
		var mutOp token.Token
		if expr.Op == token.LAND {
			mutOp = token.LOR
		} else {
			mutOp = token.LAND
		}
		// Capture operand type so Rewrite can emit closures with the right
		// return type (e.g. `func() MyBool` for named-bool operands). Both
		// operands share the same type by boolOperands' Identical check.
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

// Rewrite wraps each operand in a closure with the operand type as the
// return type, preserving short-circuit semantics. For named-bool operands
// (e.g. `type MyBool bool`) the closure is `func() MyBool { return op }`
// so the generic dispatcher infers T=MyBool.
func (BoolLogic) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	expr := c.Node.(*ast.BinaryExpr)
	var opcode string
	if expr.Op == token.LAND {
		opcode = "__cAnd"
	} else {
		opcode = "__cOr"
	}
	args := []ast.Expr{
		expr.X,
		boolFuncLit(expr.Y),
		&ast.Ident{Name: opcode},
	}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	return &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutBool"}, Args: args}
}

// BoolNot finds unary ! expressions on bool operands and proposes removal of the !.
type BoolNot struct{}

func (BoolNot) Name() string          { return "bool_not" }
func (BoolNot) DispatcherKey() string { return "bool_not" }

func (BoolNot) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate
	ast.Inspect(file, func(n ast.Node) bool {
		expr, ok := n.(*ast.UnaryExpr)
		if !ok {
			return true
		}
		if expr.Op != token.NOT {
			return true
		}
		if !boolOperand(info, expr.X) || !boolResultInferable(info, expr, expr.X) {
			return true
		}
		candidates = append(candidates, mutation.Candidate{
			Node:     expr,
			Pos:      expr.OpPos,
			Original: "!",
			Mutant:   "",
		})
		return true
	})
	return candidates
}

// Rewrite replaces !x with __cMutNot(x, mutIDs...) — no closure needed since ! doesn't short-circuit.
func (BoolNot) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	expr := c.Node.(*ast.UnaryExpr)
	args := []ast.Expr{expr.X}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	return &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutNot"}, Args: args}
}

// boolFuncLit wraps expr in func() bool { return bool(expr) }. The conversion
// accepts plain and named bools alike, so no type name is needed. Preserves
// the original node pointer so astutil.Apply can recurse into nested bool
// expressions and rewrite them too.
func boolFuncLit(expr ast.Expr) *ast.FuncLit {
	conv := &ast.CallExpr{Fun: &ast.Ident{Name: "bool"}, Args: []ast.Expr{expr}}
	return &ast.FuncLit{
		Type: &ast.FuncType{
			Params: &ast.FieldList{},
			Results: &ast.FieldList{
				List: []*ast.Field{
					{Type: &ast.Ident{Name: "bool"}},
				},
			},
		},
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.ReturnStmt{Results: []ast.Expr{conv}},
			},
		},
	}
}
