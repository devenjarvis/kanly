package ignorebad

func Add(a, b int) int {
	return a + b //kanly:ignore int_arth
}
