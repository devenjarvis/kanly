package boolsample_test

import (
	"testing"

	"github.com/devenjarvis/cauldron/internal/runner/testdata/boolsample"
)

func TestBoth(t *testing.T) {
	if !boolsample.Both(true, true) {
		t.Fatal("Both(true, true) should be true")
	}
	if boolsample.Both(true, false) {
		t.Fatal("Both(true, false) should be false")
	}
}

func TestEither(t *testing.T) {
	if !boolsample.Either(false, true) {
		t.Fatal("Either(false, true) should be true")
	}
	if boolsample.Either(false, false) {
		t.Fatal("Either(false, false) should be false")
	}
}

func TestNegate(t *testing.T) {
	if !boolsample.Negate(false) {
		t.Fatal("Negate(false) should be true")
	}
	if boolsample.Negate(true) {
		t.Fatal("Negate(true) should be false")
	}
}
