package cmpsample

import "testing"

func TestIsPositive(t *testing.T) {
	if !IsPositive(1) {
		t.Error("IsPositive(1) should be true")
	}
	if IsPositive(0) {
		t.Error("IsPositive(0) should be false")
	}
	if IsPositive(-1) {
		t.Error("IsPositive(-1) should be false")
	}
}
