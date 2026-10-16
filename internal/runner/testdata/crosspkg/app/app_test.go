package app

import "testing"

func TestArea(t *testing.T) {
	if got := Area(3); got != 9 {
		t.Fatalf("Area(3) = %d, want 9", got)
	}
}

func TestReport(t *testing.T) { Report(4) }
