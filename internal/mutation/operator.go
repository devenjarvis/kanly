package mutation

import (
	"go/ast"
	"go/token"
	"go/types"
)

type Candidate struct {
	Node     ast.Node
	Pos      token.Pos // raw position; resolve with the package FileSet
	Original string
	Mutant   string
	// Type is the contextual type of Node, set by operators whose rewrite
	// depends on it (int_literal outside plain int); nil otherwise.
	Type types.Type
}

type Operator interface {
	Name() string
	// DispatcherKey returns the name of the generated dispatcher function family
	// (e.g. "int_arith" → __cMutInt, "int_cmp" → __cMutIntCmp).
	// Candidates from operators with the same DispatcherKey on the same AST node
	// are merged into a single call site.
	DispatcherKey() string
	Find(file *ast.File, info *types.Info) []Candidate
	// Rewrite returns the replacement AST node for the given candidate.
	// mutIDs contains all mutation IDs assigned to this call site (≥1).
	Rewrite(c Candidate, mutIDs []int) ast.Node
}

// Wrapper is implemented by operators whose Rewrite wraps the candidate
// expression in a dispatcher call without inspecting its shape (for example
// return_zero's __cMutRetZero(expr, ...)). Such an operator can share a node
// with one shape-specific operator: the rewriter applies the shape-specific
// rewrite first and then wraps its result, so every mutant reaches the schema.
type Wrapper interface {
	Operator
	WrapsExpr()
}

// GroupRewriter is implemented by operators whose call site needs every
// candidate placed on it, not just the first: their mutants differ in more
// than the ID the dispatcher switches on (int_literal's sized form spells each
// mutant value at the call site; switch_case's mutants each name a clause).
// The rewriter calls RewriteGroup instead of Rewrite; cands[i] has ID
// mutIDs[i], and every cands[i].Node is the node being replaced.
type GroupRewriter interface {
	Operator
	RewriteGroup(cands []Candidate, mutIDs []int) ast.Node
}

var registry []Operator

func Register(op Operator) {
	registry = append(registry, op)
}

func Operators() []Operator {
	out := make([]Operator, len(registry))
	copy(out, registry)
	return out
}

// ResetRegistry clears the operator registry; for tests only.
func ResetRegistry() {
	registry = nil
}
