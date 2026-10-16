package tagsample

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatal("Add(2, 3) != 5")
	}
}

func TestDoubleLong(t *testing.T) {
	if testing.Short() {
		t.Skip("long test")
	}
	if Double(3) != 6 {
		t.Fatal("Double(3) != 6")
	}
}
