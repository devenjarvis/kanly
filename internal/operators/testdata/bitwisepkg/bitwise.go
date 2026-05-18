package bitwisepkg

// Each of the six bitwise/shift ops should produce exactly one int_bitwise candidate.
func And(a, b int) int    { return a & b }
func Or(a, b int) int     { return a | b }
func Xor(a, b int) int    { return a ^ b }
func Shl(a, b int) int    { return a << b }
func Shr(a, b int) int    { return a >> b }
func AndNot(a, b int) int { return a &^ b }

// Sized-int variants must be skipped by the strict-identity policy.
func AndUint(a, b uint) uint { return a & b }
func ShlInt32(a, b int32) int32 {
	return a << b
}

// Compile-time constants must be skipped (both operands constant).
const All = 0xff & 0x0f
