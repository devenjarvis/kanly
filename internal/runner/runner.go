package runner

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/devenjarvis/cauldron/internal/mutation"
)

// CompileTestBinary compiles a test binary for pkgPath using the given overlay file.
// Returns the binary path and a cleanup function.
func CompileTestBinary(ctx context.Context, pkgPath, overlayPath string) (binaryPath string, cleanup func(), err error) {
	tmpDir, err := os.MkdirTemp("", "cauldron-bin-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup = func() { os.RemoveAll(tmpDir) }
	defer func() {
		if err != nil {
			cleanup()
		}
	}()

	binPath := filepath.Join(tmpDir, "testbin")
	cmd := exec.CommandContext(ctx,
		"go", "test", "-c",
		"-overlay="+overlayPath,
		"-o", binPath,
		pkgPath,
	)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("go test -c: %w\n%s", err, out)
	}

	if _, statErr := os.Stat(binPath); statErr != nil {
		return "", nil, fmt.Errorf("test binary not found (package has no tests?): %w", statErr)
	}

	return binPath, cleanup, nil
}

// RunMutant executes the compiled test binary with CAULDRON_MUTANT=mutID.
// Returns the status, names of killing tests, elapsed duration, and any exec error.
func RunMutant(ctx context.Context, binaryPath string, mutID int, timeout time.Duration) (mutation.Status, []string, time.Duration, error) {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, binaryPath, "-test.v")
	cmd.Env = append(os.Environ(), fmt.Sprintf("CAULDRON_MUTANT=%d", mutID))

	start := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)

	if tctx.Err() == context.DeadlineExceeded {
		return mutation.StatusTimeout, nil, elapsed, nil
	}

	var killingTests []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "--- FAIL: ") {
			name := strings.TrimPrefix(line, "--- FAIL: ")
			// Strip timing info, e.g. "TestFoo (0.00s)"
			if idx := strings.Index(name, " ("); idx >= 0 {
				name = name[:idx]
			}
			killingTests = append(killingTests, strings.TrimSpace(name))
		}
	}

	if err != nil {
		return mutation.StatusKilled, killingTests, elapsed, nil
	}
	return mutation.StatusSurvived, nil, elapsed, nil
}
