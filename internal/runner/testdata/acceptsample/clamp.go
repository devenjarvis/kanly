package acceptsample

// Clamp limits n to [0, max].
func Clamp(n, max int) int {
	if n < 0 {
		return 0
	}
	if n > max {
		return max
	}
	return n
}
