package runner

import (
	"reflect"
	"testing"
	"time"
)

func TestParseTestDurations(t *testing.T) {
	out := []byte(`=== RUN   TestFast
--- PASS: TestFast (0.00s)
=== RUN   TestSlow
=== RUN   TestSlow/case
    --- PASS: TestSlow/case (1.20s)
--- PASS: TestSlow (1.25s)
=== RUN   TestSkipped
    x_test.go:9: skipping
--- SKIP: TestSkipped (0.01s)
PASS
`)
	want := map[string]time.Duration{
		"TestFast":    0,
		"TestSlow":    1250 * time.Millisecond,
		"TestSkipped": 10 * time.Millisecond,
	}
	if got := parseTestDurations(out); !reflect.DeepEqual(got, want) {
		t.Errorf("parseTestDurations = %v, want %v", got, want)
	}
}

func TestBaselineMutantTimeout(t *testing.T) {
	b := Baseline{
		Tests:     []string{"TestA", "TestB"},
		Durations: map[string]time.Duration{"TestA": time.Second, "TestB": 3 * time.Second},
		Wall:      5 * time.Second, // 1s outside test bodies
	}
	if got := b.Overhead(); got != time.Second {
		t.Errorf("Overhead = %v, want 1s", got)
	}
	tests := []struct {
		name  string
		tests []string
		want  time.Duration
	}{
		{"one test", []string{"TestA"}, 2*(time.Second+time.Second) + 500*time.Millisecond},
		{"all tests when none named", nil, 2*(4*time.Second+time.Second) + 500*time.Millisecond},
		{"unknown test counts as zero", []string{"TestGone"}, 2*time.Second + 500*time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := b.MutantTimeout(tt.tests, 2, 500*time.Millisecond); got != tt.want {
				t.Errorf("MutantTimeout(%v) = %v, want %v", tt.tests, got, tt.want)
			}
		})
	}

	parallel := Baseline{Durations: map[string]time.Duration{"TestA": 2 * time.Second, "TestB": 2 * time.Second}, Wall: time.Second}
	if got := parallel.Overhead(); got != 0 {
		t.Errorf("Overhead with parallel tests = %v, want 0", got)
	}
}
