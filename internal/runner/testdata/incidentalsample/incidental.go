package incidentalsample

func Add(a, b int) int {
	return a + b
}

// Helper exists only as a side-effect path that incidentally calls Add(0, 0).
// Any test asserting on Helper's output will hit Add's return line once, but
// because both `0 + 0` and `0 - 0` evaluate to 0 the assertion can never
// distinguish the +→- mutation. This mirrors the real-world pattern: tests
// that touch a diff line via a fixture / helper rather than through an
// assertion that exercises the line's behaviour.
func Helper() int {
	return Add(0, 0)
}
