package operators_test

import (
	"testing"

	_ "github.com/devenjarvis/kanly/internal/operators"
	"github.com/devenjarvis/kanly/internal/mutation"
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

func TestRegistryIncludesErrReturnNil(t *testing.T) {
	for _, op := range mutation.Operators() {
		if op.Name() == "err_return_nil" {
			return
		}
	}
	t.Error("err_return_nil not registered")
}
