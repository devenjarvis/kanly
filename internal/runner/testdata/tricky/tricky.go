// Package tricky collects constructs that once produced a mutant schema that
// failed to compile. Every function here must survive a rewrite with all
// operators enabled; see TestSchemaCompilesTrickyConstructs.
package tricky

import (
	bp "github.com/devenjarvis/kanly/internal/runner/testdata/tricky/boolpkg"
)

// Named string types: a "red" literal in a Color context must stay a Color.
type Color string

const Red Color = "red"

func paint(c Color) Color { return c }

func ColorName(c Color) string {
	switch c {
	case "green":
		return "G"
	case Red:
		return "R"
	}
	var d Color = "blue"
	return string(paint("cyan") + d)
}

// Integer type arguments are types, not indexable values.
type Box[T any] struct{ V T }

func Ident[T any](v T) T { return v }

func Generic(n int) int {
	var b Box[int]
	b.V = n
	return Ident[int](b.V) + 1
}

// Named bools: in the same package, function-local, and via an aliased import.
type MyBool bool

func NamedBool(a, b MyBool, n int) MyBool {
	var c MyBool = n < 5
	var e MyBool = !(n > 3)
	var f MyBool = n < 1 && n < 2
	return a && b || c && e || f && !a
}

func LocalBool(n int) bool {
	type lb bool
	var x, y lb = n > 1, n > 2
	return bool(x && y)
}

func ImportedBool(a, b bp.Flag) bp.Flag {
	return a && !b
}

// Positions Go requires to hold constants.
func ConstPositions(i int) []string {
	s := []string{0: "a", 1: "b"}
	var arr [len("abc")]int
	arr[i] = 1
	m := map[string]int{"k": arr[i]}
	return append(s, string(rune('a'+m["k"])))
}

// Nodes targeted by a shape-specific operator and a wrapper at once.
func At(s []int, i, j int) int     { return s[i+j] }
func Tail(s []int, i int) []int    { return s[i+1:] }
func From(s, a []int, i int) []int { return s[a[i]:] }

// recover() only works as a direct call from the deferred function; wrapping
// it in a dispatcher closure makes the baseline itself panic.
func Recovered() (ok bool) {
	defer func() {
		recover()
	}()
	defer func() {
		if r := recover(); r != nil && !ok {
			ok = true
			panic(r)
		}
	}()
	panic("boom")
}
