package namedincdecpkg

// MyInt is a named int type. IncDec mutates operations on it under the
// underlying-integer policy (Underlying() + Identical for symmetric operands).
type MyInt int

// NamedInc: increment on named-int. Expected: 1 candidate.
func NamedInc() {
	var x MyInt = 1
	x++
}

// NamedDec: decrement on named-int. Expected: 1 candidate.
func NamedDec() {
	var x MyInt = 5
	x--
}
