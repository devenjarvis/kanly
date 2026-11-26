// Package concurrentkill has one function whose only test checks it from a
// goroutine, and one checked by a plain test.
package concurrentkill

func Double(n int) int {
	return n * 2
}

func Triple(n int) int {
	return n * 3
}
