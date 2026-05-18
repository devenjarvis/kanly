package intlitpkg

// MagicNumber returns the magic int 42. Expected: 5 mutants (0, 1, -42, 43, 41).
func MagicNumber() int { return 42 }

// Zero returns 0. Expected: 2 mutants (1, -1) — 0, -0, 0+1=1 collapse to {1, -1}.
func Zero() int { return 0 }

// One returns 1. Expected: 3 mutants (0, -1, 2) — 1, 1-1=0 collapse.
func One() int { return 1 }

// Sized-int literal must be skipped (the literal's contextual type is int64).
func Sized() int64 { return 99 }

// Const-decl initializer must be skipped — would not compile if wrapped.
const ConstAnswer = 42

// Array length must be skipped — would not compile if wrapped. Element
// initializers are intentionally absent so the test counts stay focused on
// the three mutable literals declared above.
var Buf = [4]int{}

// Named type must be skipped.
type MyInt int

func Named() MyInt { return 7 }
