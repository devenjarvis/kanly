package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// ErrReturnNil finds error-typed expressions appearing in return statement
// results and proposes replacing each one with nil. Targets the pervasive
// `if err != nil { return err }` pattern: tests that don't assert on error
// propagation will not kill these mutants.
//
// Out of scope: naked returns in named-result functions (no Results to wrap),
// and concrete error types like *MyError (strict-identity policy, mirrors
// the plain-int and plain-bool rules).
type ErrReturnNil struct{}

func (ErrReturnNil) Name() string          { return "err_return_nil" }
func (ErrReturnNil) DispatcherKey() string { return "err_return" }

func (ErrReturnNil) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate
	ast.Inspect(file, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, expr := range ret.Results {
			if ident, ok := expr.(*ast.Ident); ok && ident.Name == "nil" {
				continue
			}
			if !isErrorType(info, expr) {
				continue
			}
			candidates = append(candidates, mutation.Candidate{
				Node:     expr,
				Pos:      expr.Pos(),
				Original: "err",
				Mutant:   "nil",
			})
		}
		return true
	})
	return candidates
}

// Rewrite wraps the error expression in __cMutErr(expr, mutIDs...) — at runtime
// the dispatcher returns nil for an active mutant, or the original error otherwise.
func (ErrReturnNil) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	expr := c.Node.(ast.Expr)
	args := []ast.Expr{expr}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	return &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutErr"}, Args: args}
}
