package concurrentkill

import "testing"

func TestDoubleAsync(t *testing.T) {
	got := make(chan int)
	go func() { got <- Double(2) }()
	if v := <-got; v != 4 {
		t.Errorf("Double(2) = %d", v)
	}
}

func TestTriple(t *testing.T) {
	if v := Triple(2); v != 6 {
		t.Errorf("Triple(2) = %d", v)
	}
}
