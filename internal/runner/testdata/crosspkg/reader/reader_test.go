package reader

import (
	"os"
	"testing"
)

// TestLoad needs to run in this directory, and leaves a file behind that
// Kanly must move out of it. It never checks Halve's result.
func TestLoad(t *testing.T) {
	data, err := os.ReadFile("input.txt")
	if err != nil {
		t.Fatal(err)
	}
	_ = Halve(len(data))
	if err := os.WriteFile("scratch.txt", data, 0o644); err != nil {
		t.Fatal(err)
	}
}
