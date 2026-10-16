package runner

import (
	"bufio"
	"context"
	"fmt"
	"go/version"
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
	"github.com/devenjarvis/kanly/internal/schema"
)

// FileLine identifies a source location by absolute file path and 1-based line.
type FileLine struct {
	File string
	Line int
}

// TestHit records one test's coverage on a specific line. Hits is the summed
// execution count across all cover blocks whose [StartLine, EndLine] span
// includes the line. A hit count says nothing about whether the test asserts
// on the line, so it must not be used to decide which tests run a mutant.
type TestHit struct {
	Name string
	Hits int
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

// schemaLangFlags returns the extra `go test` flags needed to compile the
// schema for a module whose `go` directive is goVersion. Modules older than
// schema.MinLang get the package's language version raised to it; newer (or
// non-module, "") packages are left alone. Raising never crosses go1.22, so
// the loop-variable semantics of the user's code are unchanged.
func schemaLangFlags(pkgPath, goVersion string) []string {
	if goVersion == "" || version.Compare("go"+goVersion, schema.MinLang) >= 0 {
		return nil
	}
	return []string{"-gcflags=" + pkgPath + "=-lang=" + schema.MinLang}
}

// CompileTestBinary compiles a test binary for pkgPath using the given overlay file.
// goVersion is the package's module `go` directive (source.Package.GoVersion);
// see schemaLangFlags. Returns the binary path and a cleanup function.
// buildFlags are extra `go test -c` flags such as -tags.
func CompileTestBinary(ctx context.Context, pkgPath, overlayPath, goVersion string, buildFlags ...string) (binaryPath string, cleanup func(), err error) {
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
	args := []string{"test", "-c", "-vet=off", "-overlay=" + overlayPath}
	args = append(args, schemaLangFlags(pkgPath, goVersion)...)
	args = append(args, buildFlags...)
	args = append(args, "-o", binPath, pkgPath)
	cmd := exec.CommandContext(ctx, "go", args...)
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
// buildFlags are extra `go test -c` flags such as -tags.
func CompileCoverageBinary(ctx context.Context, pkgPath string, buildFlags ...string) (binaryPath string, cleanup func(), err error) {
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
	// -covermode=count records per-block execution counts (not just 0/1 as
	// the default `set` mode does); they are reported as TestHit.Hits.
	args := []string{"test", "-c", "-vet=off", "-cover", "-covermode=count", "-coverpkg=" + pkgPath}
	args = append(args, buildFlags...)
	args = append(args, "-o", binPath, pkgPath)
	cmd := exec.CommandContext(ctx, "go", args...)
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

// Baseline is the result of running a package's tests with no mutant active.
type Baseline struct {
	// Tests is the sorted, deduplicated list of top-level tests that ran.
	Tests []string
	// Durations is each top-level test's elapsed time as reported by -test.v.
	Durations map[string]time.Duration
	// Wall is the elapsed time of the whole baseline process.
	Wall time.Duration
}

// Overhead estimates the per-process cost outside test bodies — binary
// start-up, package init and TestMain — as Wall minus the summed test
// durations, floored at zero (parallel tests can sum to more than Wall).
func (b Baseline) Overhead() time.Duration {
	var sum time.Duration
	for _, d := range b.Durations {
		sum += d
	}
	if over := b.Wall - sum; over > 0 {
		return over
	}
	return 0
}

// MutantTimeout returns the timeout for a mutant run of tests (all baseline
// tests when tests is empty): factor × (their baseline time + Overhead) +
// slack. Mutants that loop forever then cost a small multiple of the time
// the tests normally take, instead of a fixed worst case.
func (b Baseline) MutantTimeout(tests []string, factor float64, slack time.Duration) time.Duration {
	if len(tests) == 0 {
		tests = b.Tests
	}
	expected := b.Overhead()
	for _, name := range tests {
		expected += b.Durations[name]
	}
	return time.Duration(factor*float64(expected)) + slack
}

// TestFailure reports a test run that exited unsuccessfully. Failed lists
// the top-level tests that reported "--- FAIL"; it is empty when the binary
// failed outside any test (TestMain, package init, a crash before a test
// reported).
type TestFailure struct {
	Failed []string
	Output string // the run's output without -test.v progress lines
}

func (e *TestFailure) Error() string {
	if len(e.Failed) == 0 {
		return "test binary failed outside any test:\n" + e.Output
	}
	return fmt.Sprintf("tests failed: %s\n%s", strings.Join(e.Failed, ", "), e.Output)
}

// RunBaseline executes the compiled test binary with no active mutant (KANLY_MUTANT unset)
// and `-test.v`, returning the tests that ran and how long they took.
// If testRunRegex is non-empty it is passed as `-test.run=<regex>`, narrowing
// the baseline to a subset of tests — useful when callers know which tests
// exercise the focused mutation set and want to skip the rest of the package.
// Tests named in skip are excluded via `-test.skip`. testArgs are extra
// test binary flags such as -test.short.
// A run whose tests fail returns a *TestFailure.
func RunBaseline(ctx context.Context, binaryPath, pkgDir string, timeout time.Duration, testRunRegex string, skip []string, testArgs ...string) (Baseline, error) {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"-test.v"}
	if testRunRegex != "" {
		args = append(args, "-test.run="+testRunRegex)
	}
	if len(skip) > 0 {
		args = append(args, "-test.skip="+buildRunRegex(skip))
	}
	args = append(args, testArgs...)
	cmd := testCommand(tctx, binaryPath, args...)
	cmd.Dir = pkgDir
	cmd.Env = filterEnv(os.Environ(), "KANLY_MUTANT")
	start := time.Now()
	out, err := runTestCommand(cmd)
	wall := time.Since(start)

	if tctx.Err() == context.DeadlineExceeded {
		return Baseline{}, fmt.Errorf("baseline run timed out after %v", timeout)
	}
	if err != nil {
		return Baseline{}, &TestFailure{Failed: dedupe(parseTestNames(out, "--- FAIL: ")), Output: failureOutput(out)}
	}

	inventory := dedupe(parseTestNames(out, "--- PASS: ", "--- FAIL: ", "--- SKIP: "))
	sort.Strings(inventory)
	return Baseline{Tests: inventory, Durations: parseTestDurations(out), Wall: wall}, nil
}

// RunTests runs the named tests (all tests when names is empty) with no
// active mutant and returns the top-level tests that failed. err reports a
// run that could not be judged: a timeout, or a failure outside any test.
// testArgs are extra test binary flags such as -test.short.
func RunTests(ctx context.Context, binaryPath, pkgDir string, timeout time.Duration, names []string, testArgs ...string) (failed []string, err error) {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"-test.v"}
	if len(names) > 0 {
		args = append(args, "-test.run="+buildRunRegex(names))
	}
	args = append(args, testArgs...)
	cmd := testCommand(tctx, binaryPath, args...)
	cmd.Dir = pkgDir
	cmd.Env = filterEnv(os.Environ(), "KANLY_MUTANT")
	out, runErr := runTestCommand(cmd)
	if tctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("test run timed out after %v", timeout)
	}
	if runErr == nil {
		return nil, nil
	}
	failed = dedupe(parseTestNames(out, "--- FAIL: "))
	if len(failed) == 0 {
		return nil, &TestFailure{Output: failureOutput(out)}
	}
	return failed, nil
}

// dedupe returns names without repeats, keeping first occurrences in order.
func dedupe(names []string) []string {
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// parseTestDurations extracts each top-level test's elapsed time from
// `-test.v` output lines such as "--- PASS: TestFoo (0.12s)". Subtests are
// indented and already counted in their parent's time.
func parseTestDurations(out []byte) map[string]time.Duration {
	durations := make(map[string]time.Duration)
	for _, line := range strings.Split(string(out), "\n") {
		var rest string
		switch {
		case strings.HasPrefix(line, "--- PASS: "):
			rest = strings.TrimPrefix(line, "--- PASS: ")
		case strings.HasPrefix(line, "--- SKIP: "):
			rest = strings.TrimPrefix(line, "--- SKIP: ")
		default:
			continue
		}
		open := strings.LastIndex(rest, " (")
		if open < 0 || !strings.HasSuffix(rest, "s)") {
			continue
		}
		d, err := time.ParseDuration(rest[open+2 : len(rest)-1])
		if err != nil {
			continue
		}
		durations[strings.TrimSpace(rest[:open])] = d
	}
	return durations
}

// failureOutput strips the `-test.v` progress lines (=== RUN/PAUSE/CONT and
// passing or skipped results) from a failed run's output, leaving the
// failure messages, panics and FAIL summary.
func failureOutput(out []byte) string {
	var keep []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "=== ") ||
			strings.HasPrefix(trimmed, "--- PASS: ") ||
			strings.HasPrefix(trimmed, "--- SKIP: ") {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

// RunMutant executes the compiled test binary with KANLY_MUTANT=mutID.
// If testFilter is non-empty, only the named tests run (via -test.run='^(a|b|...)$').
// Test names are regex-escaped, so callers can pass raw test identifiers.
// Returns the status, names of killing tests, elapsed duration, and any exec error.
// testArgs are extra test binary flags such as -test.short.
func RunMutant(ctx context.Context, binaryPath string, mutID int, pkgDir string, testFilter []string, timeout time.Duration, testArgs ...string) (mutation.Status, []string, time.Duration, error) {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"-test.v"}
	if len(testFilter) > 0 {
		args = append(args, "-test.run="+buildRunRegex(testFilter))
	}
	args = append(args, testArgs...)
	cmd := testCommand(tctx, binaryPath, args...)
	cmd.Dir = pkgDir
	cmd.Env = append(os.Environ(), fmt.Sprintf("KANLY_MUTANT=%d", mutID))

	start := time.Now()
	out, err := runTestCommand(cmd)
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
// (as written in the profile, before normalization), an inclusive line range,
// and the execution count for the block.
type coverBlock struct {
	File      string
	StartLine int
	EndLine   int
	Count     int
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
		blocks = append(blocks, coverBlock{File: file, StartLine: startLine, EndLine: endLine, Count: count})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return blocks, nil
}

// CollectPerTestCoverage runs each named test individually under coverage and
// returns a map from (absolute file, line) to TestHit entries describing which
// tests' statement coverage touched that line and how many times.
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
// When a test touches a line via multiple overlapping cover blocks, the
// hit counts are summed: each block is a distinct basic block in Go's
// cover instrumentation, so both firing means both contributed to the
// test's traffic on that line.
//
// Coverprofile file paths are normalized via filepath.Join(pkgDir, basename),
// which is correct for single-directory Go packages.
//
// A test that fails when run on its own is not an error: it contributes no
// coverage and is returned in the second map, test name → failure output.
// testArgs are extra test binary flags such as -test.short.
func CollectPerTestCoverage(ctx context.Context, binaryPath, pkgDir string, inventory []string, timeout time.Duration, jobs int, testArgs ...string) (map[FileLine][]TestHit, map[string]string, error) {
	if jobs < 1 {
		jobs = 1
	}

	tmpDir, err := os.MkdirTemp("", "kanly-cov-*")
	if err != nil {
		return nil, nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	failures := make([]string, len(inventory))
	localBlocks := make([][]coverBlock, len(inventory))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(jobs)
	for i, name := range inventory {
		i, name := i, name
		g.Go(func() error {
			profPath := filepath.Join(tmpDir, fmt.Sprintf("cov-%d.out", i))
			tctx, cancel := context.WithTimeout(gctx, timeout)
			defer cancel()
			args := []string{
				"-test.run=" + buildRunRegex([]string{name}),
				"-test.coverprofile=" + profPath,
				"-test.parallel=1",
			}
			cmd := testCommand(tctx, binaryPath, append(args, testArgs...)...)
			cmd.Dir = pkgDir
			// Inherit env but ensure KANLY_MUTANT is not set so coverage reflects
			// the original code path.
			cmd.Env = filterEnv(os.Environ(), "KANLY_MUTANT")
			combined, runErr := runTestCommand(cmd)
			if gctx.Err() != nil {
				return gctx.Err()
			}
			if runErr != nil {
				// The test fails when run on its own: it depends on
				// another test, is flaky, or hung. Callers exclude it.
				msg := failureOutput(combined)
				if tctx.Err() == context.DeadlineExceeded {
					msg = fmt.Sprintf("timed out after %v", timeout)
				}
				failures[i] = msg
				if failures[i] == "" {
					failures[i] = runErr.Error()
				}
				return nil
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
		return nil, nil, err
	}

	failed := make(map[string]string)
	for i, name := range inventory {
		if failures[i] != "" {
			failed[name] = failures[i]
		}
	}

	// Serial merge in inventory order keeps the result map deterministic
	// regardless of which goroutine finished first.
	out := make(map[FileLine]map[string]int)
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
					set = make(map[string]int)
					out[key] = set
				}
				set[name] += b.Count
			}
		}
	}

	result := make(map[FileLine][]TestHit, len(out))
	for k, set := range out {
		hits := make([]TestHit, 0, len(set))
		for n, c := range set {
			hits = append(hits, TestHit{Name: n, Hits: c})
		}
		sort.Slice(hits, func(i, j int) bool { return hits[i].Name < hits[j].Name })
		result[k] = hits
	}
	return result, failed, nil
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
