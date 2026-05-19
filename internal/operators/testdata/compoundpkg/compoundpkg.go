package compoundpkg

// AddAssign: += on ints. Expected: 1 candidate (+= → -=).
func AddAssign(a, b int) int {
	a += b
	return a
}

// SubAssign: -= on ints. Expected: 1 candidate (-= → +=).
func SubAssign(a, b int) int {
	a -= b
	return a
}

// MulAssign: *= on ints. Expected: 1 candidate (*= → /=).
func MulAssign(a, b int) int {
	a *= b
	return a
}

// QuoAssign: /= on ints. Expected: 1 candidate (/= → *=).
func QuoAssign(a, b int) int {
	a /= b
	return a
}

// RemAssign: %= on ints. Expected: 1 candidate (%= → *=).
func RemAssign(a, b int) int {
	a %= b
	return a
}

// IndexedAssign: LHS is a map[string]int index — the closure pattern preserves
// single-evaluation of m["k"]. Expected: 1 candidate.
func IndexedAssign(m map[string]int, key string, delta int) {
	m[key] += delta
}

// PlainAssign: regular `=` is not a compound op. Expected: 0 candidates.
func PlainAssign(a, b int) int {
	a = b
	return a
}

// FloatCompound: += on float64 must be rejected. Expected: 0 candidates.
func FloatCompound(a, b float64) float64 {
	a += b
	return a
}

// Int32Compound: sized-int variant — mutated under the underlying-integer policy. Expected: 1 candidate.
func Int32Compound(a, b int32) int32 {
	a += b
	return a
}

// BitwiseAnd / BitwiseOr / BitwiseXor / Shl / Shr / AndNot:
// bitwise compound ops are mutated by IntCompoundAssign. Expected: 1 candidate each.
func BitwiseAnd(a, b int) int   { a &= b; return a }
func BitwiseOr(a, b int) int    { a |= b; return a }
func BitwiseXor(a, b int) int   { a ^= b; return a }
func Shl(a, b int) int          { a <<= b; return a }
func Shr(a, b int) int          { a >>= b; return a }
func AndNot(a, b int) int       { a &^= b; return a }
