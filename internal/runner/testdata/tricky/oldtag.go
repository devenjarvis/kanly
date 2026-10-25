//go:build go1.6
// +build go1.6

package tricky

// A go1.N build tag also sets the file's language version since Go 1.21;
// some toolchains (go1.23.0) then reject the generic dispatcher calls here.
func OldTag(a, b int) int { return a + b }

func OldTagNewOps(f float64, ch chan int8, n int) (float64, int8) {
	var k int8 = 3
	switch n {
	case 1:
		k++
	}
	return f * 2, <-ch + k
}
