package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// SliceRange mutates the low/high/max index expressions of a slice-expression
// s[lo:hi] (or s[lo:hi:max]) to lo±1, hi±1, max±1 — the range-position
// counterpart of SliceIndex.
//
// Each present, non-constant, integer-typed index emits two candidates (+1
// and -1). All-nil-bound forms like s[:] emit zero candidates. Both two-index
// (s[lo:hi]) and three-index (s[lo:hi:max]) slice expressions are covered.
type SliceRange struct{}

func (SliceRange) Name() string { return "slice_range" }

// WrapsExpr marks SliceRange as a mutation.Wrapper: Rewrite wraps the candidate
// expression whatever its shape, so it composes with other operators on the
// same node.
func (SliceRange) WrapsExpr() {}

// DispatcherKey is distinct from SliceIndex's even though both emit
// __cMutIdx: a bound like a[i] in s[a[i]:] is itself a slice_index target,
// and sharing a key would merge the two into one group that applies only
// slice_index's rewrite. With separate keys the rewriter composes them.
func (SliceRange) DispatcherKey() string { return "slice_range" }

// rangeIndex identifies which of the slice-expression's three bounds a
// candidate targets; carried in the Original/Mutant strings for grouping
// and reporting.
const (
	rangeLow  = "lo"
	rangeHigh = "hi"
	rangeMax  = "max"
)

func (SliceRange) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate

	ast.Inspect(file, func(n ast.Node) bool {
		expr, ok := n.(*ast.SliceExpr)
		if !ok {
			return true
		}
		bounds := []struct {
			node  ast.Expr
			which string
		}{
			{expr.Low, rangeLow},
			{expr.High, rangeHigh},
			{expr.Max, rangeMax},
		}
		for _, b := range bounds {
			if b.node == nil {
				continue
			}
			if !intOperand(info, b.node) {
				continue
			}
			candidates = append(candidates,
				mutation.Candidate{Node: b.node, Pos: b.node.Pos(), Original: b.which, Mutant: "+1"},
				mutation.Candidate{Node: b.node, Pos: b.node.Pos(), Original: b.which, Mutant: "-1"},
			)
		}
		return true
	})

	return candidates
}

// Rewrite replaces the bound expression with __cMutIdx(orig, mutIDs...).
// Reuses the SliceIndex dispatcher (__cMutIdx) — both produce ±1 offsets on
// integer-typed index expressions.
func (SliceRange) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	orig := c.Node.(ast.Expr)
	args := []ast.Expr{orig}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	return &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutIdx"}, Args: args}
}
