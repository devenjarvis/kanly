package orderdep

import "testing"

var ready bool

func TestFirst(t *testing.T) { ready = true }

// TestSecond passes only after TestFirst, so it fails when run on its own.
func TestSecond(t *testing.T) {
	if !ready {
		t.Fatal("TestFirst did not run first")
	}
}
