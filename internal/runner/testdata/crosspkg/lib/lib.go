package lib

// Add is checked by lib's own test.
func Add(a, b int) int { return a + b }

// Scale is only exercised by app's tests.
func Scale(a, k int) int { return a * k }

// Unused is exercised by no test anywhere.
func Unused(a int) int { return a - 1 }

// Half is run by app's tests, which never check its result.
func Half(a int) int { return a / 2 }
