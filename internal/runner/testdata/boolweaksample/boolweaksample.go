package boolweaksample

// Weak returns a && b. The test only exercises the (true, true) case so the
// &&→|| mutation produces the same result and survives.
func Weak(a, b bool) bool { return a && b }
