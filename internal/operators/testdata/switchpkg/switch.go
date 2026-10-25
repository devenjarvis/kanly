package switchpkg

// Expected: drop case 1, drop case 2 (2); swap 1↔2, swap 2↔default (2).
func Tagged(n int) string {
	switch n {
	case 1:
		return "one"
	case 2, 3:
		return "few"
	default:
		return "many"
	}
}

// Expected: drop the first case (1); swap (1). The last case is empty with
// no default, so dropping it changes nothing.
func Tagless(n int) int {
	k := 0
	switch {
	case n > 0:
		k++
	case n < 0:
	}
	return k
}

// Expected: drop case 1 (1); identical bodies are not swapped.
func Same(n int) int {
	switch n {
	case 1:
		return 0
	default:
		return 0
	}
}

// Skipped: type switches and switches whose cases call recover.
func Skipped(x any) (r int) {
	switch x.(type) {
	case int:
		r = 1
	case string:
		r = 2
	}
	defer func() {
		switch recover() {
		case nil:
			r = 3
		default:
			r = 4
		}
	}()
	return r
}
