package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// SliceIndex mutates the index expression of a[i] to a[i+1] and a[i-1] when
// the index has static type exactly int (excluding named ints, sized ints,
// and purely-constant indices).
type SliceIndex struct{}

func (SliceIndex) Name() string          { return "slice_index" }
func (SliceIndex) DispatcherKey() string { return "slice_index" }

// Rewrite replaces the IndexExpr's index with __cMutIdx(origIndex, mutIDs...).
func (SliceIndex) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	expr := c.Node.(*ast.IndexExpr)
	args := []ast.Expr{expr.Index}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	return &ast.IndexExpr{
		X:      expr.X,
		Lbrack: expr.Lbrack,
		Index:  &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutIdx"}, Args: args},
		Rbrack: expr.Rbrack,
	}
}

func (SliceIndex) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate

	ast.Inspect(file, func(n ast.Node) bool {
		expr, ok := n.(*ast.IndexExpr)
		if !ok {
			return true
		}
		if !intOperand(info, expr.Index) {
			return true
		}
		candidates = append(candidates,
			mutation.Candidate{Node: expr, Pos: expr.Lbrack, Original: "i", Mutant: "+1"},
			mutation.Candidate{Node: expr, Pos: expr.Lbrack, Original: "i", Mutant: "-1"},
		)
		return true
	})

	return candidates
}
