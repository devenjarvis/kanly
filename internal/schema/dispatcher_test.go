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

	if !strings.Contains(src, "func __cMutInt[T __cInteger](a, b T, op int, mutIDs ...int) T") {
		t.Errorf("missing generic __cMutInt declaration in:\n%s", src)
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

	if !strings.Contains(src, "func __cMutInt[T __cInteger](a, b T, op int, mutIDs ...int) T") {
		t.Errorf("__cMutInt should be generic and variadic, got:\n%s", src)
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
		"func __cMutBool[T __cBoolean](a, b func() T, op int, mutIDs ...int) T",
		"func __cMutNot[T __cBoolean](x T, mutIDs ...int) T",
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

func TestRenderDispatcherEmitsStringFunc(t *testing.T) {
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
		{ID: 50, OperatorName: "string_literal", Original: `"hello"`, Mutant: `""`},
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
		"func __cMutString(orig string, mutIDs ...int) string",
		`return ""`,
		"return orig",
	}
	for _, want := range checks {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in:\n%s", want, src)
		}
	}
}

func TestRenderDispatcherEmitsSliceIdxFunc(t *testing.T) {
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
		{ID: 60, OperatorName: "slice_index", Original: "i", Mutant: "+1"},
		{ID: 61, OperatorName: "slice_index", Original: "i", Mutant: "-1"},
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
		"func __cMutIdx[T __cInteger](i T, mutIDs ...int) T",
		"case 60:",
		"return i + 1",
		"case 61:",
		"return i - 1",
	}
	for _, want := range checks {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in:\n%s", want, src)
		}
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

	if !strings.Contains(src, "func __cMutIntCmp[T __cInteger](a, b T, op int, mutIDs ...int) bool") {
		t.Errorf("missing generic __cMutIntCmp declaration in:\n%s", src)
	}
	if !strings.Contains(src, "case 4:") {
		t.Errorf("missing case 4 for cmp mutation in:\n%s", src)
	}
	if !strings.Contains(src, "return a <= b") {
		t.Errorf("missing 'return a <= b' in:\n%s", src)
	}
}

func TestRenderDispatcherIntCmpNegateIsIncluded(t *testing.T) {
	// byOperator must include "int_cmp_negate" in IntCmpMuts alongside "int_cmp_boundary".
	// If "int_cmp_negate" → "" in the byOperator call, the int_cmp_negate mutation is excluded.
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
		{ID: 99, OperatorName: "int_cmp_negate", Original: "<", Mutant: ">="},
	}
	src, err := schema.RenderDispatcher("mypkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}
	if !strings.Contains(src, "case 99:") {
		t.Errorf("int_cmp_negate mutation (ID 99) missing from generated source:\n%s", src)
	}
	if !strings.Contains(src, "return a >= b") {
		t.Errorf("missing 'return a >= b' for int_cmp_negate mutation:\n%s", src)
	}
}

func TestRenderDispatcherSliceRangeIsIncluded(t *testing.T) {
	// byOperator must include "slice_range" in SliceIdxMuts alongside "slice_index".
	// If "slice_range" → "" the slice_range mutation is excluded.
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
		{ID: 99, OperatorName: "slice_range", Original: "lo", Mutant: "+1"},
	}
	src, err := schema.RenderDispatcher("mypkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}
	if !strings.Contains(src, "case 99:") {
		t.Errorf("slice_range mutation (ID 99) missing from generated source:\n%s", src)
	}
}

func TestRenderDispatcherEmitsStmtSwap(t *testing.T) {
	// StmtSwapMuts must include both "inc_dec" and "int_compound_assign".
	// If "StmtSwapMuts" key → "", the entire __cMutStmt function is omitted.
	// If "inc_dec" or "int_compound_assign" → "", that operator's mutations are excluded,
	// making StmtSwapMuts empty and __cMutStmt absent.
	cases := []struct {
		name   string
		mutant mutation.Mutation
	}{
		{"inc_dec", mutation.Mutation{ID: 99, OperatorName: "inc_dec", Original: "x++", Mutant: "x--"}},
		{"int_compound_assign", mutation.Mutation{ID: 99, OperatorName: "int_compound_assign", Original: "+=", Mutant: "-="}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			muts := []mutation.Mutation{
				{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
				tc.mutant,
			}
			src, err := schema.RenderDispatcher("mypkg", muts)
			if err != nil {
				t.Fatalf("RenderDispatcher: %v", err)
			}
			if !strings.Contains(src, "__cMutStmt") {
				t.Errorf("missing __cMutStmt in generated source for %s:\n%s", tc.name, src)
			}
		})
	}
}

func TestRenderDispatcherEmitsIntBit(t *testing.T) {
	// "IntBitMuts" map key and "int_bitwise" operator name must not be mutated to "".
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
		{ID: 99, OperatorName: "int_bitwise", Original: "&", Mutant: "|"},
	}
	src, err := schema.RenderDispatcher("mypkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}
	if !strings.Contains(src, "__cMutIntBit") {
		t.Errorf("missing __cMutIntBit in generated source:\n%s", src)
	}
	if !strings.Contains(src, "case 99:") {
		t.Errorf("int_bitwise mutation ID 99 missing from generated source:\n%s", src)
	}
	if !strings.Contains(src, "return a | b") {
		t.Errorf("missing 'return a | b' for int_bitwise mutation:\n%s", src)
	}
}

func TestRenderDispatcherEmitsIntLit(t *testing.T) {
	// "IntLitMuts" map key and "int_literal" operator name must not be mutated to "".
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
		{ID: 99, OperatorName: "int_literal", Original: "5", Mutant: "0"},
	}
	src, err := schema.RenderDispatcher("mypkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}
	if !strings.Contains(src, "__cMutIntLit") {
		t.Errorf("missing __cMutIntLit in generated source:\n%s", src)
	}
	if !strings.Contains(src, "case 99:") {
		t.Errorf("int_literal mutation ID 99 missing from generated source:\n%s", src)
	}
}

func TestRenderDispatcherEmitsRetZero(t *testing.T) {
	// "RetZeroMuts" key and "return_zero"/"struct_field_zero" operator names must not → "".
	// __cMutRetZero uses a linear mutID scan (no switch), so we check function presence.
	cases := []struct {
		name   string
		mutant mutation.Mutation
	}{
		{"return_zero", mutation.Mutation{ID: 99, OperatorName: "return_zero", Original: "x", Mutant: "0"}},
		{"struct_field_zero", mutation.Mutation{ID: 99, OperatorName: "struct_field_zero", Original: "v", Mutant: "0"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			muts := []mutation.Mutation{
				{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
				tc.mutant,
			}
			src, err := schema.RenderDispatcher("mypkg", muts)
			if err != nil {
				t.Fatalf("RenderDispatcher: %v", err)
			}
			if !strings.Contains(src, "__cMutRetZero") {
				t.Errorf("missing __cMutRetZero for %s:\n%s", tc.name, src)
			}
		})
	}
}

func TestRenderDispatcherEmitsBoolLit(t *testing.T) {
	// "BoolLitMuts" key and "bool_literal" operator name must not → "".
	// __cMutBoolLit uses a linear mutID scan (no switch), so we check function presence.
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
		{ID: 99, OperatorName: "bool_literal", Original: "true", Mutant: "false"},
	}
	src, err := schema.RenderDispatcher("mypkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}
	if !strings.Contains(src, "__cMutBoolLit") {
		t.Errorf("missing __cMutBoolLit in generated source:\n%s", src)
	}
}

func TestMutantExprCaseMul(t *testing.T) {
	// The "case *:" branch of mutantExpr must NOT be omitted: if the case label
	// "*" → "", then mutantExpr({Mutant:"*"}) falls to the default "a + b"
	// instead of "a * b", producing a wrong mutant expression.
	// Check by ID so the assertion isn't fooled by the hardcoded "case __cMul:" branch.
	muts := []mutation.Mutation{
		{ID: 199, OperatorName: "int_arith", Original: "/", Mutant: "*"},
	}
	src, err := schema.RenderDispatcher("mypkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}
	if !strings.Contains(src, "case 199: return a * b") {
		t.Errorf("expected 'case 199: return a * b', got:\n%s", src)
	}
}

func TestMutantExprReturnAMinusB(t *testing.T) {
	// The "return a - b" path in mutantExpr must not be zeroed.
	// Check by ID to distinguish from the hardcoded original-case "case __cSub: return a - b".
	muts := []mutation.Mutation{
		{ID: 199, OperatorName: "int_arith", Original: "+", Mutant: "-"},
	}
	src, err := schema.RenderDispatcher("mypkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}
	if !strings.Contains(src, "case 199: return a - b") {
		t.Errorf("expected 'case 199: return a - b', got:\n%s", src)
	}
}

func TestBoolLogicExprReturnsOrExpr(t *testing.T) {
	// boolLogicExpr must return "a() || b()" for Mutant=="||".
	// The hardcoded "case __cOr: return a() || b()" in the template is a different
	// occurrence; the mutant case "case 199: return a() || b()" proves the function
	// returns the right string.
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
		{ID: 199, OperatorName: "bool_logic", Original: "&&", Mutant: "||"},
	}
	src, err := schema.RenderDispatcher("mypkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}
	if !strings.Contains(src, "case 199: return a() || b()") {
		t.Errorf("expected 'case 199: return a() || b()', got:\n%s", src)
	}
}

func TestIntCmpExprReturnsLessEqual(t *testing.T) {
	// intCmpExpr("<=") must return "a <= b"; if case "<=" → "" it returns "a < b".
	// Check by ID to distinguish from the hardcoded original-case "case __cLE: return a <= b".
	muts := []mutation.Mutation{
		{ID: 1, OperatorName: "int_arith", Original: "+", Mutant: "-"},
		{ID: 199, OperatorName: "int_cmp_boundary", Original: ">", Mutant: "<="},
	}
	src, err := schema.RenderDispatcher("mypkg", muts)
	if err != nil {
		t.Fatalf("RenderDispatcher: %v", err)
	}
	if !strings.Contains(src, "case 199: return a <= b") {
		t.Errorf("expected 'case 199: return a <= b', got:\n%s", src)
	}
}
