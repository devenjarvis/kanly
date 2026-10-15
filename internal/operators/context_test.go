package operators

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// findInSource type-checks src as package p and returns op's candidates.
func findInSource(t *testing.T, src string, op mutation.Operator) []mutation.Candidate {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	conf := types.Config{Importer: importer.Default()}
	if _, err := conf.Check("p", fset, []*ast.File{file}, info); err != nil {
		t.Fatalf("type-check: %v", err)
	}
	return op.Find(file, info)
}

// ifaceDecls declares an interface R implemented by *T.
const ifaceDecls = "type R interface{ M() }; type T struct{}; func (*T) M() {};"

// TestOperatorsSkipContextsTheirDispatcherCannotFill pins the positions where
// a rewrite would not compile or would not do what the report says: the
// dispatcher's result type doesn't match the context, Go requires a constant
// there, or the rewrite changes behaviour while no mutant is active.
func TestOperatorsSkipContextsTheirDispatcherCannotFill(t *testing.T) {
	tests := []struct {
		name string
		op   mutation.Operator
		body string
		want int
	}{
		{"string: plain string mutated", StringLiteral{}, `func f() string { return "a" }`, 1},
		{"string: named type var skipped", StringLiteral{}, `type C string; func f() C { var c C = "a"; return c }`, 0},
		{"string: named type switch case skipped", StringLiteral{}, `type C string; func f(c C) int { switch c { case "a": return 1 }; return 0 }`, 0},
		{"string: named type argument skipped", StringLiteral{}, `type C string; func g(C) {}; func f() { g("a") }`, 0},
		{"string: array length skipped", StringLiteral{}, `func f() int { var a [len("abc")]int; return len(a) }`, 0},
		{"string: slice literal value mutated", StringLiteral{}, `func f() []string { return []string{0: "a"} }`, 1},
		{"int: slice literal key skipped", IntLiteral{}, `func f() []string { return []string{3: "a"} }`, 0},
		{"int: map literal key mutated", IntLiteral{}, `func f() map[int]int { return map[int]int{3: 4} }`, 10},
		{"index: type argument skipped", SliceIndex{}, `type B[T any] struct{ v T }; func I[T any](v T) T { return v }; func f(n int) int { var b B[int]; return I[int](n) + b.v }`, 0},
		{"index: value index mutated", SliceIndex{}, `func f(s []int, i int) int { return s[i] }`, 2},
		{"cmp: plain bool mutated", IntCmpNegate{}, `func f(n int) bool { return n < 5 }`, 1},
		{"cmp: named bool context skipped", IntCmpNegate{}, `type B bool; func f(n int) B { var b B = n < 5; return b }`, 0},
		{"logic: named typed operands mutated", BoolLogic{}, `type B bool; func f(a, b B) B { return a && b }`, 1},
		{"logic: untyped left operand in named context skipped", BoolLogic{}, `type B bool; func f(n int, b B) B { return n < 1 && b }`, 0},
		{"logic: untyped operands in plain context mutated", BoolLogic{}, `func f(n int) bool { return n < 1 || n > 9 }`, 1},
		{"logic: recover in right operand skipped", BoolLogic{}, `func f(ok bool) bool { return ok && recover() != nil }`, 0},
		{"not: named typed operand mutated", BoolNot{}, `type B bool; func f(a B) B { return !a }`, 1},
		{"not: untyped operand in named context skipped", BoolNot{}, `type B bool; func f(n int) B { var b B = !(n < 1); return b }`, 0},
		{"return: same-type pointer mutated", ReturnZero{}, ifaceDecls + `func f(t *T) *T { return t }`, 1},
		{"return: concrete into interface slot skipped", ReturnZero{}, ifaceDecls + `func f(t *T) R { return t }`, 0},
		{"return: concrete into interface slot in func literal skipped", ReturnZero{}, ifaceDecls + `var g = func(t *T) R { return t }`, 0},
		{"return: interface into same interface mutated", ReturnZero{}, ifaceDecls + `func f(r R) R { return r }`, 1},
		{"return: int boxed into any mutated", ReturnZero{}, `func f(x int) any { return x }`, 1},
		{"field: same-type pointer mutated", StructFieldZero{}, ifaceDecls + `type S struct{ P *T }; func f(t *T) S { return S{P: t} }`, 1},
		{"field: concrete into interface field skipped", StructFieldZero{}, ifaceDecls + `type S struct{ R R }; func f(t *T) S { return S{R: t} }`, 0},
		{"call: plain call mutated", CallDelete{}, `func g() {}; func f() { g() }`, 1},
		{"call: recover not wrapped in a closure", CallDelete{}, `func f() { defer func() { recover() }() }`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findInSource(t, "package p\n"+tt.body+"\n", tt.op)
			if len(got) != tt.want {
				t.Errorf("%s found %d candidates, want %d", tt.op.Name(), len(got), tt.want)
			}
		})
	}
}
