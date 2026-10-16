package singlehitsample

import "testing"

// TestEnable is the only test that asserts on Enable, and it calls it once.
func TestEnable(t *testing.T) {
	var c Config
	Enable(&c)
	if !c.On {
		t.Fatal("Enable did not set On")
	}
}

// TestBulk calls Enable many times and asserts nothing about it.
func TestBulk(t *testing.T) {
	for i := 0; i < 5; i++ {
		var c Config
		Enable(&c)
	}
}
