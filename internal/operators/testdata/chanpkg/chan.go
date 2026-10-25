package chanpkg

type Pipe chan int

// Expected: the send and both receives (3).
func Relay(in <-chan int, out Pipe) int {
	out <- <-in
	return <-out
}

// Expected: the receive statement (1).
func Wait(done chan struct{}) { <-done }

// Expected: the receives in a := and a var with one name, under unary minus,
// and inside a comma-ok receive's or a select case's channel expression (5).
func Nested(in chan int, cc chan chan int) int {
	x := <-in
	var y = <-in
	select {
	case <-<-cc:
	}
	_, ok := <-<-cc
	_ = ok
	return x + y + -<-in
}

// Skipped: select communications, comma-ok receives and a receive from a
// type-parameter channel. Expected: the send inside the clause body (1).
func Skipped[C ~chan int](c C, p Pipe) (int, bool) {
	select {
	case v := <-p:
		p <- v
	case p <- 1:
	case <-p:
	case <-c:
	}
	v, ok := <-p
	var w, ok2 = <-p
	_ = <-c
	return v + w, ok && ok2
}
