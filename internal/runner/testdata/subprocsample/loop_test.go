package subprocsample

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
)

// TestCount starts `sleep 60` sharing the test's stdout. It writes the
// child's PID to $SUBPROC_PIDFILE when set, and leaves the child running
// when $SUBPROC_LEAK is set.
func TestCount(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	cmd.Stdout = os.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if f := os.Getenv("SUBPROC_PIDFILE"); f != "" {
		if err := os.WriteFile(f, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("SUBPROC_LEAK") == "" {
		defer cmd.Process.Kill()
	}
	if Count(3) != 3 {
		t.Fatal("Count(3) != 3")
	}
}
