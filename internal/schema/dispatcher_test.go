package schema_test

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/devenjarvis/cauldron/internal/mutation"
	"github.com/devenjarvis/cauldron/internal/schema"
)

func TestRenderDispatcherProducesCompilableGo(t *testing.T) {
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
	}
	src, err := schema.RenderDispatcher("simple", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}

	// Must parse as valid Go.
	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "cauldron_schema.go", src, 0)
	if err != nil {
		t.Fatalf("generated source does not parse: %v\n%s", err, src)
	}

	if !strings.Contains(src, "func __cMutInt(a, b int, op int, mutIDs ...int) int") {
		t.Errorf("missing __cMutInt declaration in:\n%s", src)
	}
	if !strings.Contains(src, "case 1:") {
		t.Errorf("missing case 1 in generated source:\n%s", src)
	}
	if !strings.Contains(src, "return a - b") {
		t.Errorf("missing mutant expression 'return a - b' in:\n%s", src)
	}
}

func TestRenderDispatcherVariadicSignature(t *testing.T) {
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
	}
	src, err := schema.RenderDispatcher("simple", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}

	if !strings.Contains(src, "func __cMutInt(a, b int, op int, mutIDs ...int) int") {
		t.Errorf("__cMutInt should be variadic, got:\n%s", src)
	}
}

func TestRenderDispatcherEmitsIntCmpFunc(t *testing.T) {
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
		{ID: 4, OperatorName: "int_cmp_boundary", Original: "<", Mutant: "<="},
	}
	src, err := schema.RenderDispatcher("mypkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}

	// Must parse as valid Go.
	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "cauldron_schema.go", src, 0)
	if err != nil {
		t.Fatalf("generated source does not parse: %v\n%s", err, src)
	}

	if !strings.Contains(src, "func __cMutIntCmp(a, b int, op int, mutIDs ...int) bool") {
		t.Errorf("missing __cMutIntCmp declaration in:\n%s", src)
	}
	if !strings.Contains(src, "case 4:") {
		t.Errorf("missing case 4 for cmp mutation in:\n%s", src)
	}
	if !strings.Contains(src, "return a <= b") {
		t.Errorf("missing 'return a <= b' in:\n%s", src)
	}
}
