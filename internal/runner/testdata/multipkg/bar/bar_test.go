package bar_test

import (
	"testing"

	"multipkg/bar"
)

func TestIsPositive(t *testing.T) {
	if !bar.IsPositive(1) {
		t.Error("IsPositive(1) should be true")
	}
	if bar.IsPositive(0) {
		t.Error("IsPositive(0) should be false")
	}
	if bar.IsPositive(-1) {
		t.Error("IsPositive(-1) should be false")
	}
}
