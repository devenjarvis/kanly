package casesample

// Kind's case expressions run for every n, but TestKind takes no case: Go's
// coverage blocks for a case start after its colon, so only the switch line
// shows which tests evaluate them. The second case spans two lines.
func Kind(n int) string {
	switch {
	case n < 0:
		return "negative"
	case n == 0,
		n > 100:
		return "edge"
	}
	return "small"
}
