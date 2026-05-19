package incdecpkg

// IdentInc: simple identifier increment. Expected: 1 candidate (++ → --).
func IdentInc() int {
	x := 0
	x++
	return x
}

// IdentDec: simple identifier decrement. Expected: 1 candidate (-- → ++).
func IdentDec() int {
	x := 10
	x--
	return x
}

// IndexedInc: index expression target. Expected: 1 candidate.
// The closure pattern preserves single-evaluation of arr[i].
func IndexedInc(arr []int, i int) {
	arr[i]++
}

// SelectorInc: selector expression target. Expected: 1 candidate.
type counter struct{ n int }

func SelectorInc(c *counter) {
	c.n++
}

// ForLoopInc: increment inside a for loop. Expected: 1 candidate.
func ForLoopInc(n int) int {
	sum := 0
	for i := 0; i < n; i++ {
		sum += i
	}
	return sum
}

// FloatInc: increment on float64 must be rejected. Expected: 0 candidates.
func FloatInc() {
	var x float64 = 1.0
	x++
}

// Int32Inc: sized-int variant — mutated under the underlying-integer policy. Expected: 1 candidate.
func Int32Inc() {
	var x int32 = 1
	x++
}
