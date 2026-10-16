package failingsample

import (
	"strings"
	"testing"
)

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatal("Add(2, 3) != 5")
	}
}

func TestBroken(t *testing.T) {
	t.Fatal("this test always fails")
}

func TestCallerName(t *testing.T) {
	Greet()
	if !strings.HasSuffix(lastCaller, ".Greet") {
		t.Fatalf("record called from %s, want Greet", lastCaller)
	}
}

var seeded bool

func TestFirst(t *testing.T) { seeded = true }

// TestSecond passes only after TestFirst ran in the same process.
func TestSecond(t *testing.T) {
	if !seeded {
		t.Fatal("TestFirst did not run first")
	}
}
