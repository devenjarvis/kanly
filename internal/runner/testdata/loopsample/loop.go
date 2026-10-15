// Package loopsample has a loop that inc_dec turns infinite (i++ → i--), so
// its mutant can only end in a timeout.
package loopsample

func Count(n int) int {
	c := 0
	for i := 0; i < n; i++ {
		c++
	}
	return c
}
