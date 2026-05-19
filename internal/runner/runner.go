package runner

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// FileLine identifies a source location by absolute file path and 1-based line.
type FileLine struct {
	File string
	Line int
}

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

// CompileCoverageBinary compiles a separate, cover-instrumented test binary
// built directly from the package's on-disk source — no overlay, no
// dispatcher. It exists only to collect per-test coverage (used by
// CollectPerTestCoverage).
//
// Why a second binary: `go test -cover` invokes the cover tool against
// real on-disk source files. The mutation binary's overlay introduces a
// synthetic kanly_schema.go that does not exist on disk, which cover
// cannot open. Splitting the two binaries also keeps the mutation binary
// free of coverage instrumentation overhead during mutant runs.
//
// Coverage of the unmutated source is exactly what we want: mutations are
// expression-level rewrites that preserve line numbers, so a test that
// touches line L on the baseline is the same test that could detect any
// mutant injected at line L.
func CompileCoverageBinary(ctx context.Context, pkgPath string) (binaryPath string, cleanup func(), err error) {
	tmpDir, err := os.MkdirTemp("", "kanly-cov-bin-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup = func() { os.RemoveAll(tmpDir) }
	defer func() {
		if err != nil {
			os.RemoveAll(tmpDir)
		}
	}()

	binPath := filepath.Join(tmpDir, "covbin")
	cmd := exec.CommandContext(ctx,
		"go", "test", "-c",
		"-vet=off",
		"-cover",
		"-coverpkg="+pkgPath,
		"-o", binPath,
		pkgPath,
	)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("go test -c -cover: %w\n%s", err, out)
	}

	if _, statErr := os.Stat(binPath); statErr != nil {
		return "", nil, fmt.Errorf("coverage binary not found (package has no tests?): %w", statErr)
	}

	return binPath, cleanup, nil
}

// RunBaseline executes the compiled test binary with no active mutant (KANLY_MUTANT unset)
// and `-test.v`, returning the sorted, deduplicated list of test names that ran.
// If testRunRegex is non-empty it is passed as `-test.run=<regex>`, narrowing
// the baseline to a subset of tests — useful when callers know which tests
// exercise the focused mutation set and want to skip the rest of the package.
// Returns an error if the baseline tests fail, indicating the package is already broken.
func RunBaseline(ctx context.Context, binaryPath, pkgDir string, timeout time.Duration, testRunRegex string) ([]string, error) {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"-test.v"}
	if testRunRegex != "" {
		args = append(args, "-test.run="+testRunRegex)
	}
	cmd := exec.CommandContext(tctx, binaryPath, args...)
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
// If testFilter is non-empty, only the named tests run (via -test.run='^(a|b|...)$').
// Test names are regex-escaped, so callers can pass raw test identifiers.
// Returns the status, names of killing tests, elapsed duration, and any exec error.
func RunMutant(ctx context.Context, binaryPath string, mutID int, pkgDir string, testFilter []string, timeout time.Duration) (mutation.Status, []string, time.Duration, error) {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"-test.v"}
	if len(testFilter) > 0 {
		args = append(args, "-test.run="+buildRunRegex(testFilter))
	}
	cmd := exec.CommandContext(tctx, binaryPath, args...)
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

// buildRunRegex returns ^(a|b|c)$ with each name regex-escaped.
func buildRunRegex(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = regexp.QuoteMeta(n)
	}
	return "^(" + strings.Join(quoted, "|") + ")$"
}

// coverBlock is one entry from a coverprofile: the file path it references
// (as written in the profile, before normalization) and an inclusive line range.
type coverBlock struct {
	File      string
	StartLine int
	EndLine   int
}

// parseCoverProfile parses Go's `-coverprofile` text format. It returns one
// entry per block whose execution count is > 0. The header line ("mode: ...")
// is required. Blocks have the form:
//
//	path/to/file.go:startLine.startCol,endLine.endCol numStmts count
//
// File paths are NOT normalized; callers are responsible for resolving them.
func parseCoverProfile(data []byte) ([]coverBlock, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	first := true
	var blocks []coverBlock
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			first = false
			if !strings.HasPrefix(line, "mode:") {
				return nil, fmt.Errorf("coverprofile missing mode header, got %q", line)
			}
			continue
		}
		if line == "" {
			continue
		}
		// Split "<file>:<range> <numStmts> <count>" — file paths may contain
		// colons on Windows but not on the unix systems we target.
		colon := strings.LastIndex(line, ":")
		if colon < 0 {
			return nil, fmt.Errorf("coverprofile line missing ':' separator: %q", line)
		}
		file := line[:colon]
		rest := line[colon+1:]
		fields := strings.Fields(rest)
		if len(fields) != 3 {
			return nil, fmt.Errorf("coverprofile line has %d fields, want 3: %q", len(fields), line)
		}
		count, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("coverprofile count not int in %q: %w", line, err)
		}
		if count == 0 {
			continue
		}
		// Range is startL.startC,endL.endC; we only need the line numbers.
		comma := strings.Index(fields[0], ",")
		if comma < 0 {
			return nil, fmt.Errorf("coverprofile range missing ',': %q", line)
		}
		startPart := fields[0][:comma]
		endPart := fields[0][comma+1:]
		dot := strings.Index(startPart, ".")
		if dot < 0 {
			return nil, fmt.Errorf("coverprofile start missing '.': %q", line)
		}
		startLine, err := strconv.Atoi(startPart[:dot])
		if err != nil {
			return nil, fmt.Errorf("coverprofile start line not int in %q: %w", line, err)
		}
		dot = strings.Index(endPart, ".")
		if dot < 0 {
			return nil, fmt.Errorf("coverprofile end missing '.': %q", line)
		}
		endLine, err := strconv.Atoi(endPart[:dot])
		if err != nil {
			return nil, fmt.Errorf("coverprofile end line not int in %q: %w", line, err)
		}
		blocks = append(blocks, coverBlock{File: file, StartLine: startLine, EndLine: endLine})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return blocks, nil
}

// CollectPerTestCoverage runs each named test individually under coverage and
// returns a map from (absolute file, line) to the names of tests whose
// statement coverage touches that line.
//
// `binaryPath` must point to a cover-instrumented binary built by
// CompileCoverageBinary (NOT the mutation binary). Each per-test run is
// bounded by `timeout`. Tests run with -test.parallel=1 to avoid
// interleaved coverage attribution when tests share state.
//
// `jobs` controls how many per-test coverage runs execute concurrently;
// callers should pass >= 1 (1 reproduces the previous serial behavior).
// Each goroutine writes its own cov-<i>.out file in `tmpDir` and parses
// it into a local map; the final merge walks inventory in order so the
// returned map is deterministic regardless of completion order.
//
// Coverprofile file paths are normalized via filepath.Join(pkgDir, basename),
// which is correct for single-directory Go packages.
func CollectPerTestCoverage(ctx context.Context, binaryPath, pkgDir string, inventory []string, timeout time.Duration, jobs int) (map[FileLine][]string, error) {
	if jobs < 1 {
		jobs = 1
	}

	tmpDir, err := os.MkdirTemp("", "kanly-cov-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	localBlocks := make([][]coverBlock, len(inventory))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(jobs)
	for i, name := range inventory {
		i, name := i, name
		g.Go(func() error {
			profPath := filepath.Join(tmpDir, fmt.Sprintf("cov-%d.out", i))
			tctx, cancel := context.WithTimeout(gctx, timeout)
			defer cancel()
			cmd := exec.CommandContext(tctx, binaryPath,
				"-test.run="+buildRunRegex([]string{name}),
				"-test.coverprofile="+profPath,
				"-test.parallel=1",
			)
			cmd.Dir = pkgDir
			// Inherit env but ensure KANLY_MUTANT is not set so coverage reflects
			// the original code path.
			cmd.Env = filterEnv(os.Environ(), "KANLY_MUTANT")
			combined, runErr := cmd.CombinedOutput()
			if runErr != nil {
				return fmt.Errorf("coverage run for %q failed: %w\n%s", name, runErr, combined)
			}

			data, err := os.ReadFile(profPath)
			if err != nil {
				return fmt.Errorf("read coverprofile for %q: %w", name, err)
			}
			blocks, err := parseCoverProfile(data)
			if err != nil {
				return fmt.Errorf("parse coverprofile for %q: %w", name, err)
			}
			localBlocks[i] = blocks
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Serial merge in inventory order keeps the result map deterministic
	// regardless of which goroutine finished first.
	out := make(map[FileLine]map[string]struct{})
	for i, name := range inventory {
		for _, b := range localBlocks[i] {
			base := filepath.Base(b.File)
			if base == "kanly_schema.go" {
				continue
			}
			abs := filepath.Join(pkgDir, base)
			for ln := b.StartLine; ln <= b.EndLine; ln++ {
				key := FileLine{File: abs, Line: ln}
				set, ok := out[key]
				if !ok {
					set = make(map[string]struct{})
					out[key] = set
				}
				set[name] = struct{}{}
			}
		}
	}

	result := make(map[FileLine][]string, len(out))
	for k, set := range out {
		names := make([]string, 0, len(set))
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
		result[k] = names
	}
	return result, nil
}

// filterEnv returns a copy of env with all entries for the given key removed.
func filterEnv(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			continue
		}
		out = append(out, e)
	}
	return out
}
