// Package ignoresample mixes operators and carries a //kanly:ignore
// directive, for the suppression flags' end-to-end test.
package ignoresample

func Add(a, b int) int {
	return a + b
}

func Scale(a int) int {
	return a * 3 //kanly:ignore int_arith -- the factor is checked elsewhere
}

func Positive(a int) bool {
	return a > 0
}
