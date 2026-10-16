package ignorepkg

func Plain(a, b int) int {
	return a + b
}

func Trailing(a, b int) int {
	return a + b //kanly:ignore -- equivalent here
}

func Above(a, b int) int {
	//kanly:ignore int_arith
	return a - b
}

func OtherOp(a, b int) bool {
	//kanly:ignore int_arith
	return a < b
}

func Both(a, b int) bool {
	return a*b < b //kanly:ignore int_arith, int_cmp_boundary
}

// Whole is not mutated at all.
//
//kanly:ignore
func Whole(a, b int) int {
	if a > b {
		return a * b
	}
	return a / b
}
