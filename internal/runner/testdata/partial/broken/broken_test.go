package broken

import "testing"

func TestSub(t *testing.T) {
	if Sub(5, 3) != 3 {
		t.Fatal("deliberately wrong expectation")
	}
}
