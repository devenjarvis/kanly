package ignoresample

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatal("Add(2, 3) != 5")
	}
}

func TestScale(t *testing.T) {
	if Scale(2) != 6 {
		t.Fatal("Scale(2) != 6")
	}
}

func TestPositive(t *testing.T) {
	if !Positive(1) || Positive(0) || Positive(-1) {
		t.Fatal("Positive is wrong")
	}
}
