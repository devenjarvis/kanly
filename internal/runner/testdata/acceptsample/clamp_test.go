package acceptsample

import "testing"

func TestClampLow(t *testing.T) {
	if got := Clamp(-1, 10); got != 0 {
		t.Fatalf("Clamp(-1, 10) = %d, want 0", got)
	}
}

func TestClampHigh(t *testing.T) {
	if got := Clamp(20, 10); got != 10 {
		t.Fatalf("Clamp(20, 10) = %d, want 10", got)
	}
}

func TestClampMid(t *testing.T) {
	if got := Clamp(5, 10); got != 5 {
		t.Fatalf("Clamp(5, 10) = %d, want 5", got)
	}
}
