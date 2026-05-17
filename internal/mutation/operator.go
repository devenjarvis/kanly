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
