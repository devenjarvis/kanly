package namedcompoundpkg

// MyInt is a named int type. IntCompoundAssign mutates operations on it
// under the underlying-integer policy (Underlying() + Identical for
// symmetric operands).
type MyInt int

// NamedAddAssign: += on named-int. Expected: 1 candidate.
func NamedAddAssign(a, b MyInt) MyInt {
	a += b
	return a
}
