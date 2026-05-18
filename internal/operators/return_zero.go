package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// ReturnZero generalises ErrReturnNil's "zero the return" pattern to additional
// types. For each result expression in a ReturnStmt whose static type matches a
// supported kind, propose replacing the whole expression with its zero value:
// int→0, string→"", bool→false, and pointer/slice/map/chan/func/non-error
// interface→nil. Tests that call a function but don't assert on the returned
// value can't kill these mutants.
//
// Rewriter interaction: this operator targets the result expression node itself.
// The schema rewriter's node→group flattening silently drops collisions where
// two operators target the same node with different DispatcherKeys (see
// internal/schema/rewriter.go:151 and the doc on CallDelete). To avoid that,
// expressions whose AST shape is also a mutation target of another operator
// are skipped here:
//   - *ast.BinaryExpr (int_arith / int_cmp_* / int_bitwise / bool_logic)
//   - *ast.UnaryExpr  (bool_not, and conservatively unary -/+/^/&/<-)
//   - *ast.BasicLit   (string_literal / int_literal)
//   - *ast.FuncLit    (niche; skipped defensively)
//
// Other return shapes (idents, selectors, index, call, deref, type assert,
// composite literals, slice exprs) are mutated freely. Error-typed returns are
// also skipped — ErrReturnNil handles those.
type ReturnZero struct{}

func (ReturnZero) Name() string          { return "return_zero" }
func (ReturnZero) DispatcherKey() string { return "return_zero" }

func (ReturnZero) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate
	ast.Inspect(file, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		if len(ret.Results) == 0 {
			return true
		}
		// Multi-value call passthrough (return f() where f returns ≥2 values)
		// cannot be wrapped in a single-value call — skip the whole statement.
		if len(ret.Results) == 1 {
			if call, ok := ret.Results[0].(*ast.CallExpr); ok {
				if tup, ok := info.TypeOf(call).(*types.Tuple); ok && tup.Len() > 1 {
					return true
				}
			}
		}
		for _, expr := range ret.Results {
			c, ok := returnZeroCandidate(info, expr)
			if !ok {
				continue
			}
			candidates = append(candidates, c)
		}
		return true
	})
	return candidates
}

// Rewrite wraps the result expression in __cMutRetZero(expr, mutIDs...).
// The generic dispatcher infers T from expr's static type and returns var-zero
// of T when an active mutID fires.
func (ReturnZero) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	expr := c.Node.(ast.Expr)
	args := []ast.Expr{expr}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	return &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutRetZero"}, Args: args}
}

// returnZeroCandidate decides whether expr is an eligible return value and, if
// so, builds the candidate. Returns (zero, false) when expr should be skipped.
func returnZeroCandidate(info *types.Info, expr ast.Expr) (mutation.Candidate, bool) {
	// Skip AST shapes targeted by other operators on the same node.
	switch expr.(type) {
	case *ast.BinaryExpr, *ast.UnaryExpr, *ast.BasicLit, *ast.FuncLit:
		return mutation.Candidate{}, false
	}
	// Skip predeclared identifiers whose value already equals the mutant.
	if ident, ok := expr.(*ast.Ident); ok {
		switch ident.Name {
		case "nil", "false", "true":
			return mutation.Candidate{}, false
		}
	}
	if isErrorType(info, expr) {
		return mutation.Candidate{}, false
	}
	t := info.TypeOf(expr)
	if t == nil {
		return mutation.Candidate{}, false
	}
	mutant := returnZeroMutant(t)
	if mutant == "" {
		return mutation.Candidate{}, false
	}
	return mutation.Candidate{
		Node:     expr,
		Pos:      expr.Pos(),
		Original: "value",
		Mutant:   mutant,
	}, true
}

// returnZeroMutant returns the textual mutant label for an eligible type, or ""
// when the type is not supported. Uses strict identity for the primitive
// kinds — named wrappers (`type MyInt int`) are excluded.
func returnZeroMutant(t types.Type) string {
	switch t {
	case types.Typ[types.Int]:
		return "0"
	case types.Typ[types.String]:
		return `""`
	case types.Typ[types.Bool]:
		return "false"
	}
	if isNilableType(t) {
		return "nil"
	}
	return ""
}
