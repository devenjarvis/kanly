package tricky

import "testing"

func TestCompiles(t *testing.T) {
	if ColorName(Red) != "R" {
		t.Fatal("ColorName(Red)")
	}
	_ = Generic(1)
	_ = NamedBool(true, false, 4)
	_ = LocalBool(3)
	_ = ImportedBool(true, false)
	_ = ConstPositions(0)
}
