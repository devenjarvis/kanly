package namedincdecpkg

// MyInt is a named int type. IncDec must skip operations on it
// (strict-identity policy: do NOT use .Underlying()).
type MyInt int

// NamedInc: increment on named-int. Expected: 0 candidates.
func NamedInc() {
	var x MyInt = 1
	x++
}

// NamedDec: decrement on named-int. Expected: 0 candidates.
func NamedDec() {
	var x MyInt = 5
	x--
}
