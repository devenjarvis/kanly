package runner

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// parseTestNames extracts test names from `go test -v` output by scanning for the
// requested status prefixes (e.g. "--- FAIL: ", "--- PASS: "). The returned slice
// preserves the order tests appeared in. Trailing timing info like " (0.00s)" is
// stripped from each name.
func parseTestNames(out []byte, prefixes ...string) []string {
	var names []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		for _, p := range prefixes {
			if !strings.HasPrefix(line, p) {
				continue
			}
			name := strings.TrimPrefix(line, p)
			if idx := strings.Index(name, " ("); idx >= 0 {
				name = name[:idx]
			}
			names = append(names, strings.TrimSpace(name))
			break
		}
	}
	return names
}

// CompileTestBinary compiles a test binary for pkgPath using the given overlay file.
// Returns the binary path and a cleanup function.
func CompileTestBinary(ctx context.Context, pkgPath, overlayPath string) (binaryPath string, cleanup func(), err error) {
	tmpDir, err := os.MkdirTemp("", "kanly-bin-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup = func() { os.RemoveAll(tmpDir) }
	defer func() {
		if err != nil {
			os.RemoveAll(tmpDir)
		}
	}()

	binPath := filepath.Join(tmpDir, "testbin")
	cmd := exec.CommandContext(ctx,
		"go", "test", "-c",
		"-vet=off",
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

// RunBaseline executes the compiled test binary with no active mutant (KANLY_MUTANT unset)
// and `-test.v`, returning the sorted, deduplicated list of test names that ran.
// Returns an error if the baseline tests fail, indicating the package is already broken.
func RunBaseline(ctx context.Context, binaryPath, pkgDir string, timeout time.Duration) ([]string, error) {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, binaryPath, "-test.v")
	cmd.Dir = pkgDir
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()

	if tctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("baseline run timed out after %v", timeout)
	}
	if err != nil {
		return nil, fmt.Errorf("baseline tests fail (package is already broken):\n%s", out)
	}

	raw := parseTestNames(out, "--- PASS: ", "--- FAIL: ", "--- SKIP: ")
	seen := make(map[string]struct{}, len(raw))
	inventory := make([]string, 0, len(raw))
	for _, n := range raw {
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		inventory = append(inventory, n)
	}
	sort.Strings(inventory)
	return inventory, nil
}

// RunMutant executes the compiled test binary with KANLY_MUTANT=mutID.
// Returns the status, names of killing tests, elapsed duration, and any exec error.
func RunMutant(ctx context.Context, binaryPath string, mutID int, pkgDir string, timeout time.Duration) (mutation.Status, []string, time.Duration, error) {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, binaryPath, "-test.v")
	cmd.Dir = pkgDir
	cmd.Env = append(os.Environ(), fmt.Sprintf("KANLY_MUTANT=%d", mutID))

	start := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)

	if tctx.Err() == context.DeadlineExceeded {
		return mutation.StatusTimeout, nil, elapsed, nil
	}

	killingTests := parseTestNames(out, "--- FAIL: ")

	if err != nil {
		return mutation.StatusKilled, killingTests, elapsed, nil
	}
	return mutation.StatusSurvived, nil, elapsed, nil
}
