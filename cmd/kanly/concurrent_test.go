package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
)

func TestConcurrentTests(t *testing.T) {
	dir := t.TempDir()
	src := `package p

import (
	"sync"
	"testing"
)

func TestGo(t *testing.T) {
	done := make(chan bool)
	go func() { done <- true }()
	<-done
}

func TestWaitGroupGo(t *testing.T) {
	var wg sync.WaitGroup
	wg.Go(func() {})
	wg.Wait()
}

func TestParallelSubtests(t *testing.T) {
	for _, n := range []int{1, 2} {
		t.Run("x", func(t *testing.T) {
			t.Parallel()
			_ = n
		})
	}
}

func TestTopLevelParallel(t *testing.T) {
	t.Parallel()
	t.Run("x", func(t *testing.T) {})
}

func TestSequentialSubtests(t *testing.T) {
	t.Run("x", func(t *testing.T) { helper(t) })
}

func TestPlain(t *testing.T) {}

func helper(t *testing.T) {
	go func() {}()
}
`
	if err := os.WriteFile(filepath.Join(dir, "p_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken_test.go"), []byte("package p\nfunc TestBroken( {"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := concurrentTests(dir)
	want := map[string]bool{"TestGo": true, "TestWaitGroupGo": true, "TestParallelSubtests": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("concurrentTests = %v, want %v", got, want)
	}
}

func TestMarkConcurrentKills(t *testing.T) {
	concurrent := map[string]bool{"TestGo": true, "other/pkg.TestRace": true}
	results := []mutation.Result{
		{Status: mutation.StatusKilled, KillingTests: []string{"TestGo"}},
		{Status: mutation.StatusKilled, KillingTests: []string{"TestGo", "other/pkg.TestRace"}},
		{Status: mutation.StatusKilled, KillingTests: []string{"TestGo", "TestPlain"}},
		{Status: mutation.StatusKilled, KillingTests: []string{"TestGo"}, Cached: true},
		{Status: mutation.StatusKilled},
		{Status: mutation.StatusSurvived, CoveringTests: []string{"TestGo"}},
		{Status: mutation.StatusTimeout, KillingTests: []string{"TestGo"}},
	}
	markConcurrentKills(results, concurrent)
	want := []bool{true, true, false, true, false, false, false}
	for i, r := range results {
		if r.ConcurrentKill != want[i] {
			t.Errorf("result %d (%s, killers %v): ConcurrentKill = %v, want %v", i, r.Status, r.KillingTests, r.ConcurrentKill, want[i])
		}
	}
}
