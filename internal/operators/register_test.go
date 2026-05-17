package operators_test

import (
	"testing"

	_ "github.com/devenjarvis/cauldron/internal/operators"
	"github.com/devenjarvis/cauldron/internal/mutation"
)

func TestRegistryIncludesBoolOperators(t *testing.T) {
	ops := mutation.Operators()

	foundLogic := false
	foundNot := false
	for _, op := range ops {
		switch op.Name() {
		case "bool_logic":
			foundLogic = true
		case "bool_not":
			foundNot = true
		}
	}

	if !foundLogic {
		t.Error("bool_logic not registered")
	}
	if !foundNot {
		t.Error("bool_not not registered")
	}
}
