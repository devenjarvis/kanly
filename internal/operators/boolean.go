package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/kanly/internal/mutation"
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
		// Capture operand type so Rewrite can emit closures with the right
		// return type (e.g. `func() MyBool` for named-bool operands). Both
		// operands share the same type by boolOperands' Identical check.
		operandType := info.Types[expr.X].Type
		candidates = append(candidates, mutation.Candidate{
			Node:     expr,
			Pos:      expr.OpPos,
			Original: expr.Op.String(),
			Mutant:   mutOp.String(),
			Type:     operandType,
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
	typeName := boolReturnTypeName(c.Type)
	args := []ast.Expr{
		boolFuncLit(expr.X, typeName),
		boolFuncLit(expr.Y, typeName),
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
		if !boolOperand(info, expr.X) {
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

// boolFuncLit wraps expr in func() T { return expr } where T is the operand
// type (typically `bool`, but possibly a named wrapper). Preserves the
// original node pointer so astutil.Apply can recurse into nested bool
// expressions and rewrite them too.
func boolFuncLit(expr ast.Expr, typeName ast.Expr) *ast.FuncLit {
	return &ast.FuncLit{
		Type: &ast.FuncType{
			Results: &ast.FieldList{
				List: []*ast.Field{
					{Type: typeName},
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

// boolReturnTypeName renders t as an ast.Expr suitable for use as a func()
// return type. Handles plain bool, named types in the current package
// (`MyBool`), and qualified named types from imports (`pkg.MyBool`). Falls
// back to plain `bool` when t is nil — backwards-compatible with operators
// that don't set Candidate.Type yet.
func boolReturnTypeName(t types.Type) ast.Expr {
	if t == nil {
		return &ast.Ident{Name: "bool"}
	}
	if basic, ok := t.(*types.Basic); ok && basic.Kind() == types.Bool {
		return &ast.Ident{Name: "bool"}
	}
	if named, ok := t.(*types.Named); ok {
		obj := named.Obj()
		if obj.Pkg() == nil {
			return &ast.Ident{Name: obj.Name()}
		}
		return &ast.SelectorExpr{
			X:   &ast.Ident{Name: obj.Pkg().Name()},
			Sel: &ast.Ident{Name: obj.Name()},
		}
	}
	return &ast.Ident{Name: "bool"}
}

