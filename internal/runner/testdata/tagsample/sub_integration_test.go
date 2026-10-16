//go:build integration

package tagsample

import "testing"

func TestSub(t *testing.T) {
	if Sub(5, 3) != 2 {
		t.Fatal("Sub(5, 3) != 2")
	}
}
