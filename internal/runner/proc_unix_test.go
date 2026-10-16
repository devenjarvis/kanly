//go:build unix

package runner_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/operators"
	"github.com/devenjarvis/kanly/internal/runner"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

// compileSubprocSample compiles the subprocsample schema and returns the
// binary, the package dir and the ID of the mutant that loops forever.
func compileSubprocSample(t *testing.T) (binary, dir string, loopID int) {
	t.Helper()
	if _, err := os.Stat("/bin/sleep"); err != nil {
		t.Skip("sleep not available")
	}
	dir = relDir(t, "testdata/subprocsample")
	pkg, err := source.Load(dir)
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	rew, err := schema.Rewrite(pkg, []mutation.Operator{operators.IncDec{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}
	for _, m := range rew.Mutations {
		if m.Line == 8 { // for i := 0; i < n; i++
			loopID = m.ID
		}
	}
	if loopID == 0 {
		t.Fatalf("no inc_dec mutation on the loop header: %v", rew.Mutations)
	}
	overlay, cleanOverlay, err := runner.BuildOverlay(rew, dir)
	if err != nil {
		t.Fatalf("BuildOverlay: %v", err)
	}
	t.Cleanup(cleanOverlay)
	binary, cleanBin, err := runner.CompileTestBinary(context.Background(), pkg.ImportPath, overlay, pkg.GoVersion)
	if err != nil {
		t.Fatalf("CompileTestBinary: %v", err)
	}
	t.Cleanup(cleanBin)
	return binary, dir, loopID
}

// processGone reports whether pid has exited (a zombie awaiting its reaper
// counts as exited).
func processGone(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return true
	}
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false // no /proc: the process answered the signal, so it lives
	}
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	return len(fields) > 0 && fields[0] == "Z"
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatalf("parse pid %q: %v", data, err)
	}
	return pid
}

func waitGone(pid int) bool {
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if processGone(pid) {
			return true
		}
	}
	return false
}

// A timed-out mutant must return at its timeout even when the tests started
// a subprocess that holds the output pipe, and that subprocess must die.
func TestRunMutantTimeoutKillsSubprocesses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	binary, dir, loopID := compileSubprocSample(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	t.Setenv("SUBPROC_PIDFILE", pidFile)

	start := time.Now()
	status, _, _, err := runner.RunMutant(context.Background(), binary, loopID, dir, nil, time.Second)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunMutant: %v", err)
	}
	if status != mutation.StatusTimeout {
		t.Errorf("status = %s, want %s", status, mutation.StatusTimeout)
	}
	if elapsed > 10*time.Second {
		t.Errorf("RunMutant took %v with a 1s timeout; it waited on the subprocess", elapsed)
	}
	if pid := readPID(t, pidFile); !waitGone(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("subprocess %d outlived the timed-out test binary", pid)
	}
}

// Tests that pass but leave a subprocess running still pass, promptly, and
// the leftover subprocess is cleaned up.
func TestRunBaselineLeakedSubprocess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	binary, dir, _ := compileSubprocSample(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	t.Setenv("SUBPROC_PIDFILE", pidFile)
	t.Setenv("SUBPROC_LEAK", "1")

	start := time.Now()
	b, err := runner.RunBaseline(context.Background(), binary, dir, time.Minute, "", nil)
	if err != nil {
		t.Fatalf("RunBaseline: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("RunBaseline took %v; it waited on the leaked subprocess", elapsed)
	}
	if len(b.Tests) != 1 || b.Tests[0] != "TestCount" {
		t.Errorf("baseline tests = %v, want [TestCount]", b.Tests)
	}
	if pid := readPID(t, pidFile); !waitGone(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("leaked subprocess %d was not cleaned up", pid)
	}
}
