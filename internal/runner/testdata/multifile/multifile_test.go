package multifile

import "testing"

func TestAll(t *testing.T) {
	if Fa(1)+Fb(1)+Fc(1)+Fd(1)+Fe(1) != 10 {
		t.Fatal("sum")
	}
}
