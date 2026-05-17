package namedcompoundpkg

// MyInt is a named int type. IntCompoundAssign must skip operations on it
// (strict-identity policy: do NOT use .Underlying()).
type MyInt int

// NamedAddAssign: += on named-int. Expected: 0 candidates.
func NamedAddAssign(a, b MyInt) MyInt {
	a += b
	return a
}
