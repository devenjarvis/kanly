// Package tagsample has code and tests behind the "integration" build tag
// and a test that skips itself under -short.
package tagsample

func Add(a, b int) int {
	return a + b
}

func Double(n int) int {
	return n * 2
}
