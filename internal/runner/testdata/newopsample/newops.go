// Package newopsample exercises the float, channel, switch and sized/named
// literal operators. Its tests kill every mutant, so one that survives means
// its dispatcher does not do what the operator promises.
package newopsample

type Celsius float64

func Scale(x, y float64) float64 { return x*y + x/y - y }

func Hot(c Celsius) bool { return c >= 30 }

func Below(a, b float32) bool { return a < b }

func First(ch chan int) int { return <-ch }

func Fill(ch chan<- string, s string) { ch <- s }

// TryRecv's select case and comma-ok receive are not mutated.
func TryRecv(ch chan int) (int, bool) {
	select {
	case v, ok := <-ch:
		return v, ok
	default:
		return 0, false
	}
}

func Grade(n int) string {
	switch {
	case n >= 90:
		return "A"
	case n >= 80:
		return "B"
	default:
		return "C"
	}
}

func Name(d int) string {
	switch d {
	case 0:
		return "zero"
	case 1, 2:
		return "small"
	}
	return "big"
}

// Steps has a default clause in the middle and a fallthrough into it.
func Steps(n int) int {
	s := 0
	switch n {
	case 1:
		s += 1
		fallthrough
	default:
		s += 10
	case 3:
		s += 100
	}
	return s
}

type ID int32

func NextID(id ID) ID { return id + 7 }

func Limit() int64 { return 100 }

func Small() uint8 { return 255 }

type On bool

func Enabled() On { return true }
