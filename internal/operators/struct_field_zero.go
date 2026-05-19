package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// StructFieldZero generalises ReturnZero's "zero the expression" pattern to
// keyed struct-literal field initializers. For each `*ast.KeyValueExpr` Value
// inside a `*ast.CompositeLit` whose type is a struct, propose replacing the
// value with the zero of its static type — exactly the same semantics as
// ReturnZero, just at a different AST position.
//
// Why: tests that construct a struct but never observe a specific field can't
// catch a mutation where that field's initializer is silently zeroed.
//
// Dispatcher sharing: this operator's DispatcherKey is "return_zero" so it
// reuses the existing generic `__cMutRetZero[T any]` helper. No template
// change is required; the dispatcher wires both `return_zero` and
// `struct_field_zero` mutations through the same key.
//
// Scope: only keyed struct literals (`Foo{X: expr}`). Positional struct
// literals (`Foo{a, b}`) are out of scope; slice/array/map composite
// literals are not struct types and are excluded.
type StructFieldZero struct{}

func (StructFieldZero) Name() string          { return "struct_field_zero" }
func (StructFieldZero) DispatcherKey() string { return "return_zero" }

func (StructFieldZero) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate
	ast.Inspect(file, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		t := info.TypeOf(cl)
		if t == nil {
			return true
		}
		if _, ok := t.Underlying().(*types.Struct); !ok {
			return true
		}
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			c, ok := returnZeroCandidate(info, kv.Value)
			if !ok {
				continue
			}
			candidates = append(candidates, c)
		}
		return true
	})
	return candidates
}

// Rewrite wraps the field's value expression in __cMutRetZero(expr, mutIDs...).
// Identical to ReturnZero.Rewrite — the dispatcher helper is shared.
func (StructFieldZero) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	expr := c.Node.(ast.Expr)
	args := []ast.Expr{expr}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	return &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutRetZero"}, Args: args}
}
