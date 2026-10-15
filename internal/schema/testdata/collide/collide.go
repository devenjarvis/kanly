// Package collide holds expressions targeted by more than one operator with
// different dispatchers on the very same AST node.
package collide

// s[i+j] is both a return_zero result and a slice_index target.
func At(s []int, i, j int) int { return s[i+j] }

// i+1 is both an int_arith operand and a slice_range bound.
func Tail(s []int, i int) []int { return s[i+1:] }

// a[i] is both a slice_index target and a slice_range bound.
func From(s, a []int, i int) []int { return s[a[i]:] }
