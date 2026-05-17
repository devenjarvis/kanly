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
	Find(file *ast.File, info *types.Info) []Candidate
	// Rewrite returns the replacement AST node for the given candidate at mutID.
	// The schema rewriter calls this to construct the dispatcher call expression.
	Rewrite(c Candidate, mutID int) ast.Node
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
