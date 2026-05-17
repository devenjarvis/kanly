package uncoveredsample

import "testing"

func TestLive(t *testing.T) {
	if Live(2, 3) != 5 {
		t.Fatal("Live(2,3) != 5")
	}
}
