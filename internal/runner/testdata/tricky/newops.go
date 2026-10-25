package tricky

import (
	"errors"
	"time"

	bp "github.com/devenjarvis/kanly/internal/runner/testdata/tricky/boolpkg"
)

// Floats: named, sized, mixed with untyped constants, in a named-bool context.
type Temp float64

func Fahrenheit(c Temp) Temp { return c*9/5 + 32 }

func Ratio(a, b float32) (float32, MyBool) {
	var hot MyBool = a > b
	return a / b, hot && a >= 1.5
}

func FloatGeneric[F ~float64](x, y F) bool { return x-y < 0.5 }

// Channels: named and directional channel types, generic element types,
// select cases, comma-ok receives, receives as statements and arguments.
type Pipe chan int

func Channels(p Pipe, in <-chan string, out chan<- string) (int, string, bool) {
	p <- 1
	n := <-p + 1
	out <- <-in
	var s string
	select {
	case s = <-in:
	case v, ok := <-p:
		_, _ = v, ok
	case out <- "full":
	}
	p <- n
	v, ok := <-p
	go func() { p <- 3; p <- 4; p <- 5 }()
	var w, ok2 = <-p
	_, _, _ = v, w, ok2
	<-p
	return Ident(<-p), s, ok
}

func Elem[T any](c chan T) T {
	go func() { var zero T; c <- zero }()
	return <-c
}

func Core[C ~chan int](c C) int {
	c <- 1
	return <-c
}

func Nested(cc chan chan int) int {
	for v := range <-cc {
		return v
	}
	return 0
}

// Switches: tagged with init, untyped constant tag, interface tag with nil,
// converted named-bool cases, labeled break, a terminating switch, generic tags, type
// switches, recover in a case and comments between clauses.
func Switches(n int, err error, b bp.Flag) (string, int) {
	s := ""
	switch m := n % 4; m {
	case 0, 1: // low
		s += "low"
		fallthrough
	case 2:
		s += "+"
	default:
		// none
	}
	switch 1 {
	case n:
		s += "one"
	}
	switch err {
	case nil:
		s += "ok"
	case errors.ErrUnsupported:
		s += "unsupported"
	}
	switch {
	case bool(b):
		s += "b"
	case bool(!b) && n > 0:
		s += "!b"
	}
	k := 0
loop:
	for i := 0; i < 3; i++ {
		switch i {
		case 1:
			break loop
		case 2:
			continue
		}
		k++
	}
	switch {
	}
	switch n {
	default:
		k++
	}
	switch n {
	case 7:
	case 8:
	}
	return s, k
}

func Terminating(n int) int {
	switch n {
	case 1:
		return 10
	default:
		return 0
	}
}

func GenericTag[T comparable](v, a, b T) int {
	switch v {
	case a:
		return 1
	case b:
		return 2
	}
	return 0
}

func TypeSwitch(x any) int {
	switch v := x.(type) {
	case int:
		return v
	case string:
		return len(v)
	}
	return -1
}

func RecoverCase() (r int) {
	defer func() {
		switch recover() {
		case nil:
			r = 1
		default:
			r = 2
		}
	}()
	panic("x")
}

// Literals of sized and named types: conversions, shifts, comparisons,
// generic arguments, durations, switch cases, map keys, struct fields,
// constant expressions that wrap and negative extremes.
type Level int16

type Rec struct {
	A int16
	B uint32
}

func Pair[T any](a, b T) T { return b }

func Literals(x int32, y int64, u uint8, mb MyBool) (int64, time.Duration, Level, Rec, map[int8]string, MyBool) {
	var lv Level = 3
	z := int64(5) + y<<3
	var sh uint = 1 << y
	ok := x == 4 && u > 200
	p := Pair(x, 5)
	d := 5*time.Second + 250*time.Millisecond
	switch x {
	case 1, 2:
		lv++
	}
	var lo int8 = -128
	var wrap int32 = 1 << 40 >> 20
	var div int8 = 100 * 2 / 4
	var mbl MyBool = mb && true
	_ = Pair(mb, true)
	return z + int64(p) + int64(sh) + int64(lo) + int64(wrap) + int64(div), d, lv, Rec{A: 7, B: 1 << 31},
		map[int8]string{1: "a", -2: "b"}, mbl || MyBool(ok)
}

// Neither operand of the subtraction carries its own type, so a generic
// dispatcher would infer int where the context wants int64.
func Mask(bits uint) (int64, float32) {
	return 1<<bits - 1, 1.5 * float32(bits)
}
