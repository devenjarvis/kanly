package newopsample

import "testing"

func TestFloats(t *testing.T) {
	if got := Scale(4, 2); got != 8 {
		t.Errorf("Scale(4, 2) = %v, want 8", got)
	}
	if !Hot(30) || Hot(29) {
		t.Errorf("Hot(30), Hot(29) = %v, %v; want true, false", Hot(30), Hot(29))
	}
	if !Below(1, 2) || Below(2, 2) {
		t.Errorf("Below(1, 2), Below(2, 2) = %v, %v; want true, false", Below(1, 2), Below(2, 2))
	}
}

func TestChannels(t *testing.T) {
	ch := make(chan int, 1)
	ch <- 5
	if got := First(ch); got != 5 {
		t.Errorf("First = %d, want 5", got)
	}
	out := make(chan string, 1)
	Fill(out, "x")
	select {
	case s := <-out:
		if s != "x" {
			t.Errorf("Fill sent %q, want x", s)
		}
	default:
		t.Error("Fill sent nothing")
	}
	if v, ok := TryRecv(ch); v != 0 || ok {
		t.Errorf("TryRecv(empty) = %d, %v; want 0, false", v, ok)
	}
	ch <- 3
	if v, ok := TryRecv(ch); v != 3 || !ok {
		t.Errorf("TryRecv = %d, %v; want 3, true", v, ok)
	}
}

func TestSwitches(t *testing.T) {
	for n, want := range map[int]string{90: "A", 89: "B", 80: "B", 79: "C"} {
		if got := Grade(n); got != want {
			t.Errorf("Grade(%d) = %q, want %q", n, got, want)
		}
	}
	for d, want := range map[int]string{0: "zero", 1: "small", 2: "small", 3: "big", -1: "big"} {
		if got := Name(d); got != want {
			t.Errorf("Name(%d) = %q, want %q", d, got, want)
		}
	}
	for n, want := range map[int]int{1: 11, 2: 10, 3: 100} {
		if got := Steps(n); got != want {
			t.Errorf("Steps(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestLiterals(t *testing.T) {
	if got := NextID(1); got != 8 {
		t.Errorf("NextID(1) = %d, want 8", got)
	}
	if got := Limit(); got != 100 {
		t.Errorf("Limit() = %d, want 100", got)
	}
	if got := Small(); got != 255 {
		t.Errorf("Small() = %d, want 255", got)
	}
	if !Enabled() {
		t.Error("Enabled() = false")
	}
}
