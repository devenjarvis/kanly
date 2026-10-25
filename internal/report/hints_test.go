package report

import (
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	_ "github.com/devenjarvis/kanly/internal/operators" // registers the operators
)

// TestEveryOperatorHasAHint guards the LLM brief's hint legend and the SARIF
// rule help against an operator added without a hint, or a hint keyed by a
// name no operator has.
func TestEveryOperatorHasAHint(t *testing.T) {
	registered := map[string]bool{}
	for _, op := range mutation.Operators() {
		registered[op.Name()] = true
		if operatorHints[op.Name()] == "" {
			t.Errorf("operator %s has no hint", op.Name())
		}
	}
	for name := range operatorHints {
		if !registered[name] {
			t.Errorf("hint for unknown operator %q", name)
		}
	}
}
