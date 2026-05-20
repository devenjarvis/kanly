package incidentalsample

import "testing"

// TestAddTable exercises Add directly with inputs that distinguish the +→-
// mutation. Coverage on Add's return line: 3 hits (one per case).
func TestAddTable(t *testing.T) {
	cases := []struct {
		a, b, want int
	}{
		{1, 1, 2},
		{2, 3, 5},
		{4, 6, 10},
	}
	for _, c := range cases {
		if got := Add(c.a, c.b); got != c.want {
			t.Errorf("Add(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestHelperBehavior asserts on Helper's output but reaches Add only via the
// fixture's Add(0, 0) call. Coverage on Add's return line: 1 hit, and the
// assertion `== 0` can never distinguish +→- on the (0, 0) input. This is
// the incidental-coverage case we want to exclude from scope analysis.
func TestHelperBehavior(t *testing.T) {
	if Helper() != 0 {
		t.Errorf("Helper() != 0")
	}
}
