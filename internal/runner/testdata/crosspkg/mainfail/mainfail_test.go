package mainfail

import (
	"os"
	"testing"
)

// TestMain fails outside any test, so Kanly cannot use this package's
// tests and must skip them as a dependent of lib.
func TestMain(m *testing.M) { os.Exit(1) }

func TestSum(t *testing.T) {
	if Sum(1, 2) != 3 {
		t.Fatal("Sum(1, 2) != 3")
	}
}
