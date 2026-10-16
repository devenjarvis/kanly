package casesample

import "testing"

func TestKind(t *testing.T) {
	if got := Kind(5); got != "small" {
		t.Errorf("Kind(5) = %q, want small", got)
	}
}
