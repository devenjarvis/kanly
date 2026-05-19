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
// Strict-identity type policy: the literal's contextual type must be exactly
// `types.Typ[types.Bool]` — named bool wrappers (`type MyBool bool`) and
// untyped-only positions (e.g. `var x = true` where x infers to bool but
// the literal itself is untyped) are excluded. This mirrors `IntLiteral`
// and keeps the rewrite type-safe: the dispatcher helper returns plain
// `bool` and cannot satisfy a named-bool context without a conversion.
//
// Skipped positions (would not compile if wrapped):
//   - Const declarations — initializers must be constant expressions.
//   - Array type lengths and import specs — defensive; bool literals never
//     legally appear here, but the shared `litMutable` walker rejects them.
type BoolLiteral struct{}

func (BoolLiteral) Name() string          { return "bool_literal" }
func (BoolLiteral) DispatcherKey() string { return "bool_literal" }

// Rewrite wraps the bool keyword in __cMutBoolLit(<orig>, mutIDs...).
func (BoolLiteral) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	expr := c.Node.(ast.Expr)
	args := []ast.Expr{expr}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	return &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutBoolLit"}, Args: args}
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
			if (obj == trueObj || obj == falseObj) && litMutable(stack) && boolLitOperand(info, ident) {
				mutant := "true"
				if ident.Name == "true" {
					mutant = "false"
				}
				candidates = append(candidates, mutation.Candidate{
					Node:     ident,
					Pos:      ident.Pos(),
					Original: ident.Name,
					Mutant:   mutant,
				})
			}
		}
		stack = append(stack, n)
		return true
	})
	return candidates
}

// boolLitOperand reports whether the predeclared true/false identifier's
// contextual type has been resolved to exactly types.Bool. Untyped contexts
// (where the literal would default to plain bool but isn't yet typed) and
// named bool types (MyBool) both return false — the rewrite cannot safely
// produce a function call returning bool in those positions.
func boolLitOperand(info *types.Info, ident *ast.Ident) bool {
	tv, ok := info.Types[ident]
	if !ok {
		return false
	}
	return tv.Type == types.Typ[types.Bool]
}
