package foo_test

import (
	"testing"

	"multipkg/foo"
)

func TestAdd(t *testing.T) {
	if foo.Add(1, 2) != 3 {
		t.Error("Add(1,2) != 3")
	}
}

func TestSub(t *testing.T) {
	if foo.Sub(3, 1) == 0 {
		t.Error("Sub(3,1) should not be zero")
	}
}
