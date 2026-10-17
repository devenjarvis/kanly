package main

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devenjarvis/kanly/internal/runner"
)

// Pipelines sharing a directory run one after another; others overlap.
func TestDirLocks(t *testing.T) {
	l := newDirLocks()
	locked := make(chan func())
	go func() { locked <- l.lock([]string{"/a", "/b", "/a"}) }() // a directory named twice is locked once
	var unlock func()
	select {
	case unlock = <-locked:
	case <-time.After(5 * time.Second):
		t.Fatal("locking a directory named twice deadlocked")
	}

	disjoint := make(chan struct{})
	go func() {
		defer close(disjoint)
		l.lock([]string{"/c"})()
	}()
	select {
	case <-disjoint:
	case <-time.After(5 * time.Second):
		t.Fatal("lock on a disjoint directory waited for another")
	}

	overlapping := make(chan struct{})
	go func() {
		defer close(overlapping)
		l.lock([]string{"/c", "/b"})()
	}()
	select {
	case <-overlapping:
		t.Fatal("lock on an overlapping set did not wait")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case <-overlapping:
	case <-time.After(5 * time.Second):
		t.Fatal("lock on an overlapping set still waits after the release")
	}
}

// Callers asking for overlapping sets in different orders do not deadlock.
func TestDirLocksOrderIndependent(t *testing.T) {
	l := newDirLocks()
	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(2)
			go func() { defer wg.Done(); l.lock([]string{"/x", "/y"})() }()
			go func() { defer wg.Done(); l.lock([]string{"/y", "/x"})() }()
		}
		wg.Wait()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("overlapping locks taken in different orders deadlocked")
	}
}

func TestLongestFirst(t *testing.T) {
	cost := []time.Duration{0, 3, 1, 3, 2, 9}
	idx := []int{1, 2, 3, 4}
	longestFirst(idx, cost)
	if want := []int{1, 3, 4, 2}; !slices.Equal(idx, want) { // 1 and 3 tie and keep their order
		t.Errorf("longestFirst = %v, want %v", idx, want)
	}
}

func TestFailureStage(t *testing.T) {
	cause := errors.New("boom")
	for _, tt := range []struct {
		err   error
		stage string
	}{
		{&stageError{"compile", cause}, "compile"},
		{fmt.Errorf("wrapped: %w", &stageError{"baseline", cause}), "baseline"},
		{cause, "mutants"},
	} {
		stage, got := failureStage(tt.err)
		if stage != tt.stage || !errors.Is(got, cause) {
			t.Errorf("failureStage(%v) = %q, %v; want %q, %v", tt.err, stage, got, tt.stage, cause)
		}
	}
}

func TestExpectedCost(t *testing.T) {
	own := suite{baseline: runner.Baseline{
		Tests:     []string{"TestA", "TestB"},
		Durations: map[string]time.Duration{"TestA": time.Second, "TestB": 3 * time.Second},
		Wall:      5 * time.Second, // 1s outside the tests
	}}
	dep := suite{baseline: runner.Baseline{
		Tests:     []string{"TestC"},
		Durations: map[string]time.Duration{"TestC": 10 * time.Second},
		Wall:      10 * time.Second,
	}}
	suites := []suite{own, dep}
	for _, tt := range []struct {
		name  string
		tests [][]string
		want  time.Duration
	}{
		{"one test", [][]string{{"TestA"}, nil}, 2 * time.Second},
		{"both tests", [][]string{{"TestA", "TestB"}, nil}, 5 * time.Second},
		{"both suites", [][]string{{"TestB"}, {"TestC"}}, 14 * time.Second},
		{"a suite without tests costs nothing", [][]string{nil, nil}, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := expectedCost(suites, tt.tests); got != tt.want {
				t.Errorf("expectedCost = %v, want %v", got, tt.want)
			}
		})
	}
}

// Concurrent writers' lines arrive whole.
func TestSyncWriter(t *testing.T) {
	var buf bytes.Buffer
	w := &syncWriter{w: &buf}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				fmt.Fprintf(w, "writer %02d line %02d\n", i, j)
			}
		}()
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 1000 {
		t.Fatalf("%d lines, want 1000", len(lines))
	}
	for _, l := range lines {
		if len(l) != len("writer 00 line 00") || !strings.HasPrefix(l, "writer ") {
			t.Fatalf("garbled line %q", l)
		}
	}
}

// pipelineSpans runs Kanly and returns, per package import path, the
// positions in its progress output of the package's first line ("…
// compiling") and of its "done" line.
func pipelineSpans(t *testing.T, args ...string) map[string][2]int {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := run(append([]string{"--format=json"}, args...), &stdout, &stderr); code != 0 {
		t.Fatalf("run %v returned %d; stderr: %s", args, code, stderr.String())
	}
	spans := make(map[string][2]int)
	for i, line := range strings.Split(stderr.String(), "\n") {
		pkg, rest, ok := strings.Cut(strings.TrimPrefix(line, "kanly: "), ": ")
		if !ok {
			continue
		}
		s := spans[pkg]
		switch {
		case strings.HasSuffix(rest, "mutants; compiling"):
			s[0] = i
		case strings.HasPrefix(rest, "done in "):
			s[1] = i
		default:
			continue
		}
		spans[pkg] = s
	}
	return spans
}

// overlap reports whether the two packages' pipelines ran at the same time:
// each started before the other was done.
func overlap(t *testing.T, spans map[string][2]int, a, b string) bool {
	t.Helper()
	sa, oka := spans[a]
	sb, okb := spans[b]
	if !oka || !okb || sa[1] == 0 || sb[1] == 0 {
		t.Fatalf("progress lacks the start or end of %s or %s: %v", a, b, spans)
	}
	return sa[0] < sb[1] && sb[0] < sa[1]
}

// Packages run concurrently, up to --jobs at a time.
func TestRunPackagesConcurrently(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	a := relDir(t, "../../internal/runner/testdata/sample")
	b := relDir(t, "../../internal/runner/testdata/cmpsample")
	const pa, pb = "github.com/devenjarvis/kanly/internal/runner/testdata/sample", "github.com/devenjarvis/kanly/internal/runner/testdata/cmpsample"
	if !overlap(t, pipelineSpans(t, "--jobs=2", a, b), pa, pb) {
		t.Error("--jobs=2: the two packages ran one after the other")
	}
	if overlap(t, pipelineSpans(t, "--jobs=1", a, b), pa, pb) {
		t.Error("--jobs=1: the two packages ran at the same time")
	}
}

// A package with tests but nothing to mutate is skipped with a notice; its
// sibling is reported as usual.
func TestRunSkipsPackagesWithoutMutations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	var stdout, stderr bytes.Buffer
	args := []string{"--format=json", relDir(t, "../../internal/runner/testdata/nomutants"), relDir(t, "../../internal/runner/testdata/sample")}
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("run returned %d; stderr: %s", code, stderr.String())
	}
	if want := "skip github.com/devenjarvis/kanly/internal/runner/testdata/nomutants: no mutations"; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr lacks %q:\n%s", want, stderr.String())
	}
	if strings.Contains(stderr.String(), "testdata/sample: no mutations") {
		t.Errorf("sample, which has mutants, was skipped:\n%s", stderr.String())
	}
}

// With --cross-package, a package whose directory another package's
// dependent tests use does not run beside it.
func TestRunCrossPackageLocksDependentsDirs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	root := relDir(t, "../../internal/runner/testdata/crosspkg")
	chdir(t, root)
	t.Setenv("KANLY_CACHE_DIR", t.TempDir())
	// lib's dependents include app, so app's own tests must wait for them,
	// and other, which shares no directory with lib's run, need not.
	spans := pipelineSpans(t, "--cross-package", "--jobs=3", "./lib", "./app", "./other")
	if overlap(t, spans, "crosspkg/lib", "crosspkg/app") {
		t.Errorf("lib's dependent tests and app's own tests ran at the same time: %v", spans)
	}
	// Whichever of lib and app goes first, other starts before it is done.
	other, ok := spans["crosspkg/other"]
	if !ok || other[1] == 0 {
		t.Fatalf("progress lacks crosspkg/other: %v", spans)
	}
	if other[0] > min(spans["crosspkg/lib"][1], spans["crosspkg/app"][1]) {
		t.Errorf("other waited for lib or app although it shares no directory with them: %v", spans)
	}
}
