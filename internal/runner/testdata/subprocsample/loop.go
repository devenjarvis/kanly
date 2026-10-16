// Package subprocsample's test starts a subprocess that holds the test
// binary's output pipe, like a test that launches a helper server. inc_dec
// turns Count's loop infinite (i++ → i--), so that mutant ends in a timeout.
package subprocsample

func Count(n int) int {
	c := 0
	for i := 0; i < n; i++ {
		c++
	}
	return c
}
