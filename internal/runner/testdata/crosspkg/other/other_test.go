package other

import "testing"

func TestTwice(t *testing.T) {
	if got := Twice(4); got != 8 {
		t.Fatalf("Twice(4) = %d, want 8", got)
	}
}
