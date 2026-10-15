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
	s := []int{1, 2, 3}
	_, _, _ = At(s, 0, 1), Tail(s, 0), From(s, s, 0)
	_ = OldTag(1, 2)
	if !Recovered() {
		t.Fatal("Recovered")
	}
}
