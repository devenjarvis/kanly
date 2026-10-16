package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// pipeWaitDelay bounds how long a test process's output is drained after it
// exits or is killed. A test that starts a subprocess (a helper server, a
// container client) hands it the output pipe; without a bound, Wait blocks
// until that grandchild exits, so a timed-out mutant could stall the run for
// as long as the grandchild lives.
const pipeWaitDelay = time.Second

// testCommand returns a command that runs a compiled test binary in its own
// process group, so that killing it on timeout also kills any processes the
// tests started (see setProcessGroup).
func testCommand(ctx context.Context, binary string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.WaitDelay = pipeWaitDelay
	setProcessGroup(cmd)
	return cmd
}

// runTestCommand runs cmd (from testCommand) and returns its combined output.
// The process gets a private, freshly created temporary directory, removed
// afterwards: files tests leave in os.TempDir do not pile up, and concurrent
// runs of the same test cannot collide over a fixed temporary path.
// Processes the tests left behind are killed once the binary exits. A binary
// that passed but left a subprocess holding its output pipe is a pass: the
// leftover pipe is not the tests' verdict.
func runTestCommand(cmd *exec.Cmd) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "kanly-run-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+tmp, "TMP="+tmp, "TEMP="+tmp)

	out, err := cmd.CombinedOutput()
	killProcessGroup(cmd)
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	return out, err
}
