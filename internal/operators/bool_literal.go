package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// BoolLiteral mutates the predeclared boolean keywords `true` and `false`
// to their opposite. Targets `*ast.Ident` whose Name is "true" or "false"
// and whose `info.ObjectOf` resolves to the universe predeclared constant —
// shadowed identifiers (e.g. `true := someInt; ...`) are excluded by the
// object-identity check.
//
// Negative-shape tests assert that operator skip logic still rejects sites
// it should reject. Those skip checks usually appear as `return false` /
// `return true` statements inside `Find` helpers; flipping the literal
// toggles the skip decision, surfacing tests that exercise those paths.
//
// Type policy: the literal's contextual type must be plain `bool` or a named
// bool (`type MyBool bool`); untyped-only positions (e.g. `if true`, where
// the literal keeps its untyped type) are excluded. The dispatcher returns
// plain `bool`, so in a named-bool context the call is compared with `true`:
// a comparison is an untyped bool, which takes the type its context gives it.
//
// Skipped positions (would not compile if wrapped):
//   - Const declarations — initializers must be constant expressions.
//   - Array type lengths and import specs — defensive; bool literals never
//     legally appear here, but the shared `litMutable` walker rejects them.
type BoolLiteral struct{}

func (BoolLiteral) Name() string          { return "bool_literal" }
func (BoolLiteral) DispatcherKey() string { return "bool_literal" }

// Rewrite wraps the bool keyword in __cMutBoolLit(<orig>, mutIDs...), or in
// a named-bool context (__cMutBoolLit(<orig>, mutIDs...) == true).
func (BoolLiteral) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	expr := c.Node.(ast.Expr)
	args := []ast.Expr{expr}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	call := &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutBoolLit"}, Args: args}
	if c.Type == types.Typ[types.Bool] {
		return call
	}
	return &ast.ParenExpr{X: &ast.BinaryExpr{X: call, Op: token.EQL, Y: ast.NewIdent("true")}}
}

func (BoolLiteral) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate
	var stack []ast.Node

	trueObj := types.Universe.Lookup("true")
	falseObj := types.Universe.Lookup("false")

	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if ident, ok := n.(*ast.Ident); ok && (ident.Name == "true" || ident.Name == "false") {
			obj := info.ObjectOf(ident)
			if t := boolLitType(info, ident); (obj == trueObj || obj == falseObj) && t != nil && litMutable(stack) {
				mutant := "true"
				if ident.Name == "true" {
					mutant = "false"
				}
				candidates = append(candidates, mutation.Candidate{
					Node:     ident,
					Pos:      ident.Pos(),
					Original: ident.Name,
					Mutant:   mutant,
					Type:     t,
				})
			}
		}
		stack = append(stack, n)
		return true
	})
	return candidates
}

// boolLitType returns the predeclared true/false identifier's contextual
// type when it is plain bool or a named bool, or nil when it stays untyped.
func boolLitType(info *types.Info, ident *ast.Ident) types.Type {
	tv := info.Types[ident]
	if !isBoolType(tv.Type) || tv.Type == types.Typ[types.UntypedBool] {
		return nil
	}
	return tv.Type
}
