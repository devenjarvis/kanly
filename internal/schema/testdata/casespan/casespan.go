package casespan

func F(n int, ch chan int) int {
	switch n + 1 {
	case n * 2,
		n - 3:
		return n + 4
	}
	select {
	case ch <- n + 5:
		return n + 6
	default:
	}
	switch {
	case func() bool {
		switch {
		case n+7 > 0:
		}
		return n+8 > 0
	}():
	}
	return n + 9
}
