package boolweaksample_test

import (
	"testing"

	"github.com/devenjarvis/kanly/internal/runner/testdata/boolweaksample"
)

func TestWeak(t *testing.T) {
	// Only the (true, true) case is tested; both a&&b and a||b return true here,
	// so the &&→|| mutation is not detected.
	if !boolweaksample.Weak(true, true) {
		t.Fatal("Weak(true, true) should be true")
	}
}
