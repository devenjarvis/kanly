package flakykill

import (
	"os"
	"testing"
)

// TestDouble asserts nothing about Double. Under a mutant, it fails once
// per $FLAKY_MARKER file, then passes.
func TestDouble(t *testing.T) {
	Double(1)
	marker := os.Getenv("FLAKY_MARKER")
	if marker == "" || os.Getenv("KANLY_MUTANT") == "" {
		return
	}
	if _, err := os.Stat(marker); err != nil {
		_ = os.WriteFile(marker, nil, 0o644)
		t.Fatal("collided with another test process")
	}
}
