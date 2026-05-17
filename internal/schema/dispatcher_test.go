package schema_test

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/schema"
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
	_, err = parser.ParseFile(fset, "kanly_schema.go", src, 0)
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

func TestRenderDispatcherIncludesRemCase(t *testing.T) {
	muts := []mutation.Mutation{
		{ID: 3, OperatorName: "int_arith", Original: "%", Mutant: "*"},
	}
	src, err := schema.RenderDispatcher("rempkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "kanly_schema.go", src, 0)
	if err != nil {
		t.Fatalf("generated source does not parse: %v\n%s", err, src)
	}

	if !strings.Contains(src, "case __cRem:") {
		t.Errorf("missing 'case __cRem:' in original-op switch:\n%s", src)
	}
	if !strings.Contains(src, "return a % b") {
		t.Errorf("missing 'return a %% b' in original-op switch:\n%s", src)
	}
	// case 3 fires the mutant (* instead of %)
	if !strings.Contains(src, "case 3:") {
		t.Errorf("missing 'case 3:' for rem→mul mutant:\n%s", src)
	}
	if !strings.Contains(src, "return a * b") {
		t.Errorf("missing 'return a * b' for rem→mul mutant:\n%s", src)
	}
}

func TestRenderDispatcherEmitsBoolFuncs(t *testing.T) {
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
		{ID: 21, OperatorName: "bool_logic", Original: "&&", Mutant: "||"},
		{ID: 22, OperatorName: "bool_not", Original: "!", Mutant: ""},
	}
	src, err := schema.RenderDispatcher("mypkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "kanly_schema.go", src, 0)
	if err != nil {
		t.Fatalf("generated source does not parse: %v\n%s", err, src)
	}

	checks := []string{
		"func __cMutBool(a, b func() bool, op int, mutIDs ...int) bool",
		"func __cMutNot(x bool, mutIDs ...int) bool",
		"case 21:",
		"return a() || b()",
		"case 22:",
		"return x",
		"return !x",
		"__cAnd",
		"__cOr",
	}
	for _, want := range checks {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in:\n%s", want, src)
		}
	}
}

func TestRenderDispatcherOmitsBoolFuncsWhenAbsent(t *testing.T) {
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
	}
	src, err := schema.RenderDispatcher("mypkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}

	if strings.Contains(src, "__cMutBool") {
		t.Errorf("expected no __cMutBool in int-only dispatcher:\n%s", src)
	}
	if strings.Contains(src, "__cMutNot") {
		t.Errorf("expected no __cMutNot in int-only dispatcher:\n%s", src)
	}
}

func TestRenderDispatcherBoolSectionsAreIndependent(t *testing.T) {
	intArith := mutation.Mutation{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"}

	t.Run("bool_logic only emits __cMutBool not __cMutNot", func(t *testing.T) {
		muts := []mutation.Mutation{intArith, {ID: 21, OperatorName: "bool_logic", Original: "&&", Mutant: "||"}}
		src, err := schema.RenderDispatcher("mypkg", muts)
		if err != nil {
			t.Fatalf("RenderDispatcher: %v", err)
		}
		if !strings.Contains(src, "__cMutBool") {
			t.Errorf("expected __cMutBool when bool_logic mutations present:\n%s", src)
		}
		if strings.Contains(src, "__cMutNot") {
			t.Errorf("expected no __cMutNot when only bool_logic mutations present:\n%s", src)
		}
	})

	t.Run("bool_not only emits __cMutNot not __cMutBool", func(t *testing.T) {
		muts := []mutation.Mutation{intArith, {ID: 22, OperatorName: "bool_not", Original: "!", Mutant: ""}}
		src, err := schema.RenderDispatcher("mypkg", muts)
		if err != nil {
			t.Fatalf("RenderDispatcher: %v", err)
		}
		if strings.Contains(src, "__cMutBool") {
			t.Errorf("expected no __cMutBool when only bool_not mutations present:\n%s", src)
		}
		if !strings.Contains(src, "__cMutNot") {
			t.Errorf("expected __cMutNot when bool_not mutations present:\n%s", src)
		}
	})
}

func TestRenderDispatcherEmitsErrFunc(t *testing.T) {
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
		{ID: 30, OperatorName: "err_return_nil", Original: "err", Mutant: "nil"},
	}
	src, err := schema.RenderDispatcher("mypkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "kanly_schema.go", src, 0)
	if err != nil {
		t.Fatalf("generated source does not parse: %v\n%s", err, src)
	}

	checks := []string{
		"func __cMutErr(x error, mutIDs ...int) error",
		"case 30:",
		"return nil",
		"return x",
	}
	for _, want := range checks {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in:\n%s", want, src)
		}
	}
}

func TestRenderDispatcherOmitsErrFuncWhenAbsent(t *testing.T) {
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
	}
	src, err := schema.RenderDispatcher("mypkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}

	if strings.Contains(src, "__cMutErr") {
		t.Errorf("expected no __cMutErr in int-only dispatcher:\n%s", src)
	}
}

func TestRenderDispatcherEmitsCallSkipFunc(t *testing.T) {
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
		{ID: 40, OperatorName: "call_delete", Original: "os.RemoveAll(...)", Mutant: ""},
	}
	src, err := schema.RenderDispatcher("mypkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "kanly_schema.go", src, 0)
	if err != nil {
		t.Fatalf("generated source does not parse: %v\n%s", err, src)
	}

	checks := []string{
		"func __cMutCallSkip(fn func(), mutIDs ...int)",
		"id == __kanlyActiveMutant",
		"fn()",
	}
	for _, want := range checks {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in:\n%s", want, src)
		}
	}
}

func TestRenderDispatcherOmitsCallSkipFuncWhenAbsent(t *testing.T) {
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
	}
	src, err := schema.RenderDispatcher("mypkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}

	if strings.Contains(src, "__cMutCallSkip") {
		t.Errorf("expected no __cMutCallSkip in int-only dispatcher:\n%s", src)
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
	_, err = parser.ParseFile(fset, "kanly_schema.go", src, 0)
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
