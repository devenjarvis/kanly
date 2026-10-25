package intlitpkg

// MagicNumber returns the magic int 42. Expected: 5 mutants (0, 1, -42, 43, 41).
func MagicNumber() int { return 42 }

// Zero returns 0. Expected: 2 mutants (1, -1) — 0, -0, 0+1=1 collapse to {1, -1}.
func Zero() int { return 0 }

// One returns 1. Expected: 3 mutants (0, -1, 2) — 1, 1-1=0 collapse.
func One() int { return 1 }

// Sized-int literal (contextual type int64). Expected: 5 mutants.
func Sized() int64 { return 99 }

// Unsigned literal: -n and n-1 = -1 are not representable. Expected: 1
// mutant (1).
func Unsigned() uint8 { return 0 }

// Boundary of a sized type: n+1 overflows int8. Expected: 4 mutants
// (0, 1, -127, 126).
func MaxInt8() int8 { return 127 }

// Untyped shift count and float context must be skipped.
func Skipped(x int64) float64 { return float64(x<<3) + 2 }

// Const-decl initializer must be skipped — would not compile if wrapped.
const ConstAnswer = 42

// Array length must be skipped — would not compile if wrapped. Element
// initializers are intentionally absent so the test counts stay focused on
// the three mutable literals declared above.
var Buf = [4]int{}

// Named type. Expected: 5 mutants.
type MyInt int

func Named() MyInt { return 7 }
