package floatoppkg

type Temp float64

type MyBool bool

// Expected float_arith: * / + (3), and * inside an int expression (1).
// Expected float_cmp: none.
func Fahrenheit(c Temp) Temp { return c*9/5 + 32 }

func Rounded(n int, f float64) int { return n + int(f*f) }

// Expected float_cmp_boundary: < (1); float_cmp_negate: < (1).
func Below(a, b float32) bool { return a < b }

// Expected float_cmp_negate: == (1); no boundary sibling.
func Same(a, b float64) bool { return a == b }

// Expected, inside other comparisons: boundary > > (2), negate > > (2).
func Within(n int, f float64) bool { return n < 3 && f > 0.5 || (n == 0) == (f < pick(f > 2)) }

func pick(b bool) float64 {
	if b {
		return 1
	}
	return 2
}

// Skipped: both operands constant, integer operands, a named-bool context,
// complex operands and a type-parameter operand.
func Skipped(n int, c complex128, f float64) (float64, int, MyBool, complex128) {
	var hot MyBool = f > 1
	return 1.5 * 2, n + 1, hot, c * c
}

func Generic[F ~float64](x, y F) F { return x - y }
