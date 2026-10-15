package initsample

import "testing"

func TestTotal(t *testing.T) {
	if Total != 5 {
		t.Fatalf("Total = %d, want 5", Total)
	}
}

func TestInited(t *testing.T) {
	if Inited != 6 {
		t.Fatalf("Inited = %d, want 6", Inited)
	}
}
