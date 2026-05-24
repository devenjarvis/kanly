package broken

// undeclaredVar is intentionally undefined to produce a compile error.
func Broken() int { return undeclaredVar }
