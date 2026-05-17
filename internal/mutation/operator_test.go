package mutation_test

import (
	"go/ast"
	"go/types"
	"testing"

	"github.com/devenjarvis/cauldron/internal/mutation"
)

type fakeOp struct{ name string }

func (f fakeOp) Name() string                                          { return f.name }
func (f fakeOp) Find(_ *ast.File, _ *types.Info) []mutation.Candidate { return nil }

func TestRegisterReturnsRegisteredOperators(t *testing.T) {
	mutation.ResetRegistry()

	a := fakeOp{"a"}
	b := fakeOp{"b"}
	mutation.Register(a)
	mutation.Register(b)

	ops := mutation.Operators()
	if len(ops) != 2 {
		t.Fatalf("expected 2 operators, got %d", len(ops))
	}
	if ops[0].Name() != "a" || ops[1].Name() != "b" {
		t.Errorf("unexpected operator names: %v, %v", ops[0].Name(), ops[1].Name())
	}
}
