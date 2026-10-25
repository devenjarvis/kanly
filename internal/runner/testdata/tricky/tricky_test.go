package tricky

import (
	"errors"
	"testing"
	"time"

	bp "github.com/devenjarvis/kanly/internal/runner/testdata/tricky/boolpkg"
)

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
	if m, _ := Mask(3); m != 7 {
		t.Fatal("Mask")
	}
	ch := make(chan int8, 1)
	ch <- 1
	if f, k := OldTagNewOps(1, ch, 1); f != 2 || k != 5 {
		t.Fatal("OldTagNewOps")
	}
	if !Recovered() {
		t.Fatal("Recovered")
	}
}

func TestNewOperatorConstructs(t *testing.T) {
	if got := Fahrenheit(100); got != 212 {
		t.Errorf("Fahrenheit(100) = %v", got)
	}
	if r, hot := Ratio(3, 2); r != 1.5 || !hot {
		t.Errorf("Ratio(3, 2) = %v, %v", r, hot)
	}
	if !FloatGeneric(1.0, 0.75) {
		t.Error("FloatGeneric")
	}

	p := make(Pipe, 4)
	in := make(chan string, 2)
	in <- "a"
	in <- "b"
	out := make(chan string, 1)
	if n, s, ok := Channels(p, in, out); n != 5 || s != "b" || !ok || <-out != "a" {
		t.Errorf("Channels = %d, %q, %v", n, s, ok)
	}
	if Elem(make(chan string)) != "" || Core(make(chan int, 1)) != 1 {
		t.Error("Elem, Core")
	}
	cc := make(chan chan int, 1)
	c := make(chan int, 1)
	c <- 9
	close(c)
	cc <- c
	if Nested(cc) != 9 {
		t.Error("Nested")
	}

	for _, tc := range []struct {
		n    int
		err  error
		b    bp.Flag
		want string
		k    int
	}{
		{0, nil, true, "low+okb", 2},
		{1, errors.ErrUnsupported, false, "low+oneunsupported!b", 2},
		{2, nil, false, "+ok!b", 2},
		{3, nil, false, "ok!b", 2},
	} {
		if s, k := Switches(tc.n, tc.err, tc.b); s != tc.want || k != tc.k {
			t.Errorf("Switches(%d) = %q, %d; want %q, %d", tc.n, s, k, tc.want, tc.k)
		}
	}
	if Terminating(1) != 10 || Terminating(2) != 0 {
		t.Error("Terminating")
	}
	if GenericTag("x", "y", "x") != 2 || GenericTag(1, 2, 3) != 0 {
		t.Error("GenericTag")
	}
	if TypeSwitch("abc") != 3 || TypeSwitch(4) != 4 {
		t.Error("TypeSwitch")
	}
	if RecoverCase() != 2 {
		t.Error("RecoverCase")
	}

	z, d, lv, rec, m, mb := Literals(4, 1, 201, false)
	if z != 13+5+2-128+1<<20+50 || d != 5250*time.Millisecond || lv != 3 || rec.A != 7 || rec.B != 1<<31 || m[-2] != "b" || !mb {
		t.Errorf("Literals = %d, %v, %d, %+v, %v, %v", z, d, lv, rec, m, mb)
	}
}
