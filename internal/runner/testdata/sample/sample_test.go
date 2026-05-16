package sample

import "testing"

// TestAdd checks Add precisely — kills the +→- mutant.
func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatal("Add(2,3) != 5")
	}
}

// TestSub only checks that Sub returns a non-zero value — a deliberately weak test
// that lets the -→+ mutant survive.
func TestSub(t *testing.T) {
	if Sub(5, 3) == 0 {
		t.Fatal("Sub(5,3) should not be 0")
	}
}
