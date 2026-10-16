// Package flakykill's test fails the first time it runs against a mutant
// and passes afterwards, like a test that collides with a concurrent copy
// of itself over a shared port or file.
package flakykill

func Double(n int) int {
	return n * 2
}
