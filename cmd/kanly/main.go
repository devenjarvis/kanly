package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/devenjarvis/kanly/internal/diff"
	"github.com/devenjarvis/kanly/internal/mutation"
	_ "github.com/devenjarvis/kanly/internal/operators" // register operators via init()
	"github.com/devenjarvis/kanly/internal/report"
	"github.com/devenjarvis/kanly/internal/runner"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/selector"
	"github.com/devenjarvis/kanly/internal/source"
)

// version is overridden at release-build time via
// -ldflags="-X main.version=vX.Y.Z". The "dev" default is replaced at runtime
// with the embedded module version when the binary was produced by
// `go install github.com/devenjarvis/kanly/cmd/kanly@vX.Y.Z`.
var version = "dev"

func main() {
	os.Exit(dispatch(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// dispatch routes the agent subcommands (see agent.go); anything else is a
// mutation run.
func dispatch(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "guide":
			return runGuide(args[1:], stdout, stderr)
		case "init":
			return runInit(args[1:], stdout, stderr)
		case "hook":
			return runHook(args[1:], stdin, stdout, stderr)
		case "show":
			return runShow(args[1:], stdout, stderr)
		case "test":
			return runTest(args[1:], stdout, stderr)
		}
	}
	return run(args, stdout, stderr)
}

func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

// runConfig collects the per-package knobs threaded from CLI flags into runPackage.
type runConfig struct {
	filter        func(file string, line int, funcName string) bool
	timeout       time.Duration       // fixed per-mutant timeout; 0 derives it from the baseline
	timeoutFactor float64             // multiplier on baseline test time for derived timeouts
	testsRegex    string              // if non-empty, narrows baseline + per-test coverage
	mutants       *mutantSet          // if non-nil, only these mutants run
	operators     map[string]bool     // if non-nil, only these operators' mutants run
	buildFlags    []string            // extra `go test -c` flags (-tags)
	testArgs      []string            // extra test binary flags (-test.short)
	progress      *progress           // informational output
	jobs          int                 // number of worker slots; >= 1
	slots         *semaphore.Weighted // jobs worker slots, shared by every package's test processes and compiles
	verifyKills   bool                // re-run every killed mutant alone to confirm the kill
	failFast      bool                // stop each mutant run at its first failing test
	dependents    []runner.Dependent  // other packages whose tests also run against the mutants (--cross-package)
	cache         *setupCache         // dependents' setups across runs; nil disables it
	kills         *killCache          // kills across runs; nil disables it
	moduleStamp   string              // the module's non-code files, part of the cache keys
}

// slotted returns f's results, calling it while holding one of cfg's shared
// worker slots.
func slotted[T, U any](ctx context.Context, cfg runConfig, f func() (T, U, error)) (T, U, error) {
	if err := cfg.slots.Acquire(ctx, 1); err != nil {
		var t T
		var u U
		return t, u, err
	}
	defer cfg.slots.Release(1)
	return f()
}

// mutantArgs returns the test binary flags for a run against a mutant.
// -test.failfast stops at the first failing test: the verdict is the same,
// only the kill set is partial.
func (c runConfig) mutantArgs() []string {
	if !c.failFast {
		return c.testArgs
	}
	return append(c.testArgs[:len(c.testArgs):len(c.testArgs)], "-test.failfast")
}

const (
	// defaultTimeoutFactor allows a mutant run three times its tests'
	// baseline time — headroom for --jobs contention and noisy CI machines.
	defaultTimeoutFactor = 3.0
	// timeoutSlack is added to every derived timeout so that tests whose
	// baseline rounds to 0.00s still get a usable budget.
	timeoutSlack = 2 * time.Second
	// minSetupTimeout bounds the baseline and per-test coverage runs. They
	// execute unmutated code, so they should finish; the limit only stops a
	// hung test suite from stalling the run forever.
	minSetupTimeout = 10 * time.Minute
	// defaultLLMLimit keeps the llm brief to a size an agent can work
	// through in one pass; the rest follows on the next run.
	defaultLLMLimit = 30
)

// mutantTimeout is the timeout for running tests against one mutant.
func (c runConfig) mutantTimeout(b runner.Baseline, tests []string) time.Duration {
	if c.timeout > 0 {
		return c.timeout
	}
	return b.MutantTimeout(tests, c.timeoutFactor, timeoutSlack)
}

// setupTimeout bounds the baseline and coverage runs.
func (c runConfig) setupTimeout() time.Duration {
	return max(c.timeout, minSetupTimeout)
}

// stageError tags a runPackage failure with the pipeline stage it came from;
// see report.PackageError for the stage names.
type stageError struct {
	stage string
	err   error
}

func (e *stageError) Error() string { return e.stage + ": " + e.err.Error() }
func (e *stageError) Unwrap() error { return e.err }

// failureStage splits a package's failure into the stage it failed at and
// its cause; an error without a stage failed in the mutant loop.
func failureStage(err error) (string, error) {
	var se *stageError
	if errors.As(err, &se) {
		return se.stage, se.err
	}
	return "mutants", err
}

// syncWriter serializes writes to w, so concurrent writers' lines do not
// interleave; each fmt.Fprintf is a single Write.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// dirLocks hands out one lock per directory.
type dirLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newDirLocks() *dirLocks { return &dirLocks{locks: make(map[string]*sync.Mutex)} }

// lock takes the locks of dirs, in sorted order so that two callers
// wanting overlapping sets cannot deadlock, and returns their release.
func (l *dirLocks) lock(dirs []string) (unlock func()) {
	dirs = append([]string(nil), dirs...)
	sort.Strings(dirs)
	dirs = slices.Compact(dirs)
	held := make([]*sync.Mutex, len(dirs))
	for i, d := range dirs {
		l.mu.Lock()
		m, ok := l.locks[d]
		if !ok {
			m = new(sync.Mutex)
			l.locks[d] = m
		}
		l.mu.Unlock()
		m.Lock()
		held[i] = m
	}
	return func() {
		for _, m := range held {
			m.Unlock()
		}
	}
}

// maxBaselineRounds bounds how many times the baseline is re-run with newly
// failing tests skipped. A test that panics stops the binary, hiding the
// tests after it, so one round may not reveal every failing test.
const maxBaselineRounds = 3

// establishBaseline runs the package's tests on the schema binary with no
// mutant active. Tests that fail are excluded, after re-running them alone
// on both binaries to say why, and the baseline is re-run without them. It
// fails only when the binary fails outside any test or failures persist.
func establishBaseline(ctx context.Context, schemaBin, origBin, dir string, cfg runConfig) (runner.Baseline, []report.ExcludedTest, error) {
	var skip []string
	var excluded []report.ExcludedTest
	for round := 0; ; round++ {
		b, err := runner.RunBaseline(ctx, schemaBin, dir, cfg.setupTimeout(), cfg.testsRegex, skip, cfg.testArgs...)
		if err == nil {
			if len(b.Tests) == 0 && len(skip) > 0 {
				return b, nil, errors.New("every test fails with no mutant applied")
			}
			return b, excluded, nil
		}
		var tf *runner.TestFailure
		if !errors.As(err, &tf) || len(tf.Failed) == 0 || round == maxBaselineRounds {
			return runner.Baseline{}, nil, explainBaselineFailure(ctx, origBin, dir, cfg, err)
		}
		origFailed, origErr := runner.RunTests(ctx, origBin, dir, cfg.setupTimeout(), tf.Failed, cfg.testArgs...)
		schemaFailed, schemaErr := runner.RunTests(ctx, schemaBin, dir, cfg.setupTimeout(), tf.Failed, cfg.testArgs...)
		failsOrig, failsSchema := toSet(origFailed), toSet(schemaFailed)
		for _, name := range tf.Failed {
			var reason string
			switch {
			case origErr != nil || failsOrig[name]:
				reason = "fails on the unmodified code"
			case schemaErr != nil || failsSchema[name]:
				reason = "fails only under Kanly's rewritten code; it may depend on allocations, call stacks or timing, otherwise please report it as a Kanly bug"
			default:
				reason = "failed in the baseline but passed when re-run alone: flaky, or depends on another test"
			}
			excluded = append(excluded, report.ExcludedTest{Test: name, Reason: reason})
		}
		skip = append(skip, tf.Failed...)
	}
}

// explainBaselineFailure turns a baseline failure that is not attributable
// to individual tests into an error saying whether the unmodified code fails
// too, which separates a broken test suite from a Kanly bug.
func explainBaselineFailure(ctx context.Context, origBin, dir string, cfg runConfig, err error) error {
	var tf *runner.TestFailure
	if !errors.As(err, &tf) {
		return err
	}
	if _, origErr := runner.RunTests(ctx, origBin, dir, cfg.setupTimeout(), nil, cfg.testArgs...); origErr == nil {
		return fmt.Errorf("the tests pass on the unmodified code but fail under Kanly's rewritten code; this is likely a Kanly bug, please report it:\n%s", tf.Output)
	}
	return fmt.Errorf("tests fail with no mutant applied:\n%s", tf.Output)
}

// displayPaths returns paths relative to the working directory when they
// lie under it.
func displayPaths(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = displayPath(p)
	}
	return out
}

func displayPath(p string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return p
	}
	if rel, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}

func toSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// failureMessage returns the first line of a test failure's output that is
// not a "--- FAIL" / "FAIL" status line, falling back to the first line.
func failureMessage(msg string) string {
	first := ""
	for _, line := range strings.Split(msg, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if first == "" {
			first = line
		}
		if !strings.HasPrefix(line, "--- FAIL") && !strings.HasPrefix(line, "FAIL") && !strings.HasPrefix(line, "exit status") {
			return line
		}
	}
	return first
}

// confirmTimeoutScale multiplies a mutant's timeout when a timeout is
// re-run alone to confirm it.
const confirmTimeoutScale = 2.0

// confirmVerdicts re-runs, one at a time, the verdicts most likely to be
// artifacts of running mutants in parallel. Every timeout is re-run with a
// longer budget and takes the re-run's verdict, since a loaded machine can
// push a killable or surviving mutant past its timeout. With verifyKills,
// every kill is re-run too, and a kill that does not reproduce is reported
// as survived: tests that collide over shared ports, files or databases
// when run concurrently fail without detecting anything.
// rerun is given each result's index in results.
func confirmVerdicts(results []mutation.Result, verifyKills bool, rerun func(i int, r mutation.Result, timeoutScale float64) (mutation.Result, error)) error {
	for i, r := range results {
		switch {
		case r.Status == mutation.StatusTimeout:
			again, err := rerun(i, r, confirmTimeoutScale)
			if err != nil {
				return err
			}
			if again.Status != mutation.StatusTimeout {
				again.Note = fmt.Sprintf("timed out in the parallel run; %s when re-run alone", again.Status)
			}
			results[i] = again
		case r.Status == mutation.StatusKilled && verifyKills:
			again, err := rerun(i, r, 1)
			if err != nil {
				return err
			}
			if again.Status == mutation.StatusSurvived {
				again.Note = "killed in the parallel run but survived when re-run alone; the kill was not reproducible"
				results[i] = again
			}
		}
	}
	return nil
}

// progress writes informational lines to stderr unless --quiet is set. It
// is safe for concurrent use.
type progress struct {
	mu sync.Mutex
	w  io.Writer // nil when quiet
}

func (p *progress) printf(format string, args ...any) {
	if p == nil || p.w == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.w, "kanly: "+format+"\n", args...)
}

// guardDir snapshots dir before tests run in it and returns a function
// that moves what they left behind out of it (not delete: a file created
// during the run may be the user's), warning about what cannot be undone.
func guardDir(dir, label string, stderr io.Writer) func() {
	snap, err := runner.SnapshotDir(dir)
	if err != nil {
		return func() {}
	}
	return func() {
		res, err := snap.Restore()
		if err != nil {
			fmt.Fprintf(stderr, "warning: %s: checking for files changed by tests: %v\n", label, err)
		}
		if len(res.Moved) > 0 {
			fmt.Fprintf(stderr, "%s: moved %d file(s) created during the run to %s: %s\n", label, len(res.Moved), res.Quarantine, strings.Join(displayPaths(res.Moved), ", "))
		}
		if len(res.Unmoved) > 0 {
			fmt.Fprintf(stderr, "warning: %s: %d file(s) were created during the run and could not be moved; remove them if unwanted: %s\n", label, len(res.Unmoved), strings.Join(displayPaths(res.Unmoved), ", "))
		}
		if len(res.Changed) > 0 {
			fmt.Fprintf(stderr, "warning: %s: tests modified or deleted %d file(s); check them before committing: %s\n", label, len(res.Changed), strings.Join(displayPaths(res.Changed), ", "))
		}
	}
}

// suite is one test binary run against a package's mutants: the package's
// own tests or, with --cross-package, a dependent package's.
type suite struct {
	pkg      string // import path of the package the tests belong to
	dir      string // directory the tests run in
	bin      string // test binary compiled with the mutant schema
	baseline runner.Baseline
	covMap   map[runner.FileLine][]runner.TestHit
	own      bool // the mutated package's own tests
}

// resultName is how one of the suite's tests is named in results: plain for
// the package's own tests, qualified by import path for a dependent's.
func (s suite) resultName(test string) string {
	if s.own {
		return test
	}
	return s.pkg + "." + test
}

// coveringTests returns the suite's tests that run mut's line. A mutation in
// a case expression also takes the tests of its switch or select line: a
// case expression runs whenever its statement is reached and no earlier
// case matched, not only when its case is taken. Line 0 (no CoverLine) has
// no coverage.
func (s suite) coveringTests(mut mutation.Mutation) []string {
	hits := unionHits(s.covMap[runner.FileLine{File: mut.File, Line: mut.Line}], s.covMap[runner.FileLine{File: mut.File, Line: mut.CoverLine}])
	names := make([]string, len(hits))
	for i, h := range hits {
		names[i] = h.Name
	}
	return names
}

// longestFirst sorts idx, indices into cost, by descending cost, keeping
// the order of equal costs.
func longestFirst(idx []int, cost []time.Duration) {
	sort.SliceStable(idx, func(a, b int) bool { return cost[idx[a]] > cost[idx[b]] })
}

// expectedCost estimates how long a mutant takes to test when it survives:
// for each suite with tests to run (tests[i] for suites[i]), their baseline
// time plus the suite's per-process overhead.
func expectedCost(suites []suite, tests [][]string) time.Duration {
	var total time.Duration
	for i, s := range suites {
		if len(tests[i]) > 0 {
			total += s.baseline.MutantTimeout(tests[i], 1, 0)
		}
	}
	return total
}

// runMutantSuites runs each suite's covering tests (tests[i] for suites[i])
// against mutant id. The mutant is killed if any suite's tests fail, and
// timed out if none fail but one times out. timeoutScale multiplies each
// suite's derived timeout.
func runMutantSuites(ctx context.Context, suites []suite, id int, tests [][]string, timeoutScale float64, cfg runConfig) (mutation.Status, []string, time.Duration, error) {
	status := mutation.StatusSurvived
	var killers []string
	var total time.Duration
	for i, s := range suites {
		if len(tests[i]) == 0 {
			continue
		}
		timeout := time.Duration(timeoutScale * float64(cfg.mutantTimeout(s.baseline, tests[i])))
		st, killed, dur, err := runner.RunMutant(ctx, s.bin, id, s.dir, tests[i], timeout, cfg.mutantArgs()...)
		if err != nil {
			return "", nil, 0, err
		}
		total += dur
		switch st {
		case mutation.StatusKilled:
			status = mutation.StatusKilled
			for _, name := range killed {
				killers = append(killers, s.resultName(name))
			}
			if cfg.failFast {
				return status, killers, total, nil
			}
		case mutation.StatusTimeout:
			if status != mutation.StatusKilled {
				status = mutation.StatusTimeout
			}
		}
	}
	return status, killers, total, nil
}

// reusedKiller returns the first of covering, the tests that now cover a
// mutant, that killed it in an earlier run whose entry e has the mutant's
// current key, provided the test's source is unchanged; "" when there is
// none.
func reusedKiller(e killEntry, key string, testHash map[string]string, covering []string) string {
	if key == "" || e.Key != key {
		return ""
	}
	for _, name := range covering {
		if h, ok := e.Killers[name]; ok && h != "" && h == testHash[name] {
			return name
		}
	}
	return ""
}

// killUpdates returns the kill cache entries a run's results change: a fresh
// kill is stored with its killers' source hashes, and any other verdict
// drops the mutant's entry. Reused kills and mutants without a key are left
// alone.
func killUpdates(results []mutation.Result, keys []string, testHash map[string]string) map[string]killEntry {
	set := make(map[string]killEntry)
	for i, r := range results {
		if keys[i] == "" || r.Cached {
			continue
		}
		e := killEntry{Key: keys[i]}
		if r.Status == mutation.StatusKilled {
			for _, name := range r.KillingTests {
				if h := testHash[name]; h != "" {
					if e.Killers == nil {
						e.Killers = make(map[string]string)
					}
					e.Killers[name] = h
				}
			}
		}
		set[r.Mutation.Fingerprint] = e
	}
	return set
}

// dependentSuites compiles each of cfg.dependents' tests against pkg's
// mutant schema, runs them unmutated and collects their coverage of pkg. A
// dependent that cannot be set up is skipped with a warning: its tests only
// add kills, and their failure says nothing about pkg. The returned cleanup
// removes the binaries and restores the dependents' directories.
func dependentSuites(ctx context.Context, pkg *source.Package, overlayPath string, cfg runConfig, stderr io.Writer) ([]suite, []report.ExcludedTest, func()) {
	// The cleanups are independent: binaries removed, directories restored.
	var cleanups []func()
	cleanup := func() {
		for _, c := range cleanups {
			c()
		}
	}
	var suites []suite
	var excluded []report.ExcludedTest
	for _, dep := range cfg.dependents {
		cfg.progress.printf("%s: running the tests of dependent %s unmutated", pkg.ImportPath, dep.ImportPath)
		s, ex, err := func() (suite, []report.ExcludedTest, error) {
			bin, cleanBin, err := slotted(ctx, cfg, func() (string, func(), error) {
				return runner.CompileDependentTestBinary(ctx, dep.ImportPath, pkg.ImportPath, overlayPath, pkg.GoVersion, cfg.buildFlags...)
			})
			if err != nil {
				return suite{}, nil, err
			}
			cleanups = append(cleanups, cleanBin)
			covBin, cleanCov, err := slotted(ctx, cfg, func() (string, func(), error) {
				return runner.CompileDependentCoverageBinary(ctx, dep.ImportPath, pkg.ImportPath, cfg.buildFlags...)
			})
			if err != nil {
				return suite{}, nil, err
			}
			cleanups = append(cleanups, cleanCov, guardDir(dep.Dir, dep.ImportPath, stderr))
			cache := cfg.cache
			if cfg.moduleStamp == "" {
				cache = nil
			}
			key, _ := cache.key([]string{bin, covBin}, cfg.moduleStamp, dep.Dir, pkg.Dir, cfg.testsRegex, fmt.Sprint(cfg.testArgs))
			if cached, ok := cache.load(key); ok {
				cfg.progress.printf("%s: reusing the setup of dependent %s from the cache", pkg.ImportPath, dep.ImportPath)
				return suite{pkg: dep.ImportPath, dir: dep.Dir, bin: bin, baseline: cached.Baseline, covMap: cached.covMap()}, nil, nil
			}
			baseline, ex, err := slotted(ctx, cfg, func() (runner.Baseline, []report.ExcludedTest, error) {
				return establishBaseline(ctx, bin, covBin, dep.Dir, cfg)
			})
			if err != nil {
				return suite{}, nil, err
			}
			covMap, covFailed, err := runner.CollectPerTestCoverage(ctx, covBin, dep.Dir, pkg.Dir, baseline.Tests, cfg.setupTimeout(), cfg.slots, cfg.testArgs...)
			if err != nil {
				return suite{}, nil, err
			}
			for _, name := range baseline.Tests {
				if msg, failed := covFailed[name]; failed {
					ex = append(ex, report.ExcludedTest{Test: name, Reason: "fails when run on its own (it depends on another test, is flaky, or hangs): " + failureMessage(msg)})
				}
			}
			// A failure may be transient (a loaded machine), so only a
			// clean setup is reused.
			if len(ex) == 0 {
				cache.store(key, newDependentSetup(baseline, covMap))
			}
			for i := range ex {
				ex[i].Package = dep.ImportPath
			}
			return suite{pkg: dep.ImportPath, dir: dep.Dir, bin: bin, baseline: baseline, covMap: covMap}, ex, nil
		}()
		if err != nil {
			first, _, _ := strings.Cut(err.Error(), "\n")
			fmt.Fprintf(stderr, "warning: %s: not counting the tests of dependent %s: %s\n", pkg.ImportPath, dep.ImportPath, first)
			continue
		}
		suites = append(suites, s)
		excluded = append(excluded, ex...)
	}
	return suites, excluded, cleanup
}

// pkgResult is what one package's pipeline contributes to the report.
type pkgResult struct {
	results    []mutation.Result
	ignored    int                           // mutants suppressed by //kanly:ignore
	inventory  []string                      // tests reported for the package
	excluded   []report.ExcludedTest         // tests left out because they fail unmutated
	funcRanges map[string]mutation.FuncRange // canonical func name → source range
	stale      []report.StaleIgnore          // .kanlyignore entries matching no mutant
	matched    []string                      // --mutant fingerprints found in the package
}

func runPackage(ctx context.Context, pkg *source.Package, ops []mutation.Operator, cfg runConfig, stderr io.Writer) (pkgResult, error) {
	accepted, err := schema.ReadIgnoreFile(pkg.Dir)
	if err != nil {
		return pkgResult{}, &stageError{"rewrite", err}
	}
	rew, err := schema.RewriteWith(pkg, ops, schema.Options{Filter: cfg.filter, Operators: cfg.operators, Accepted: accepted})
	if err != nil {
		return pkgResult{}, &stageError{"rewrite", err}
	}
	// Stale entries are reported even when nothing in scope is left to run.
	var stale []report.StaleIgnore
	for _, fp := range rew.StaleAccepted {
		stale = append(stale, report.StaleIgnore{
			Package:     pkg.ImportPath,
			File:        filepath.Join(pkg.Dir, schema.IgnoreFileName),
			Fingerprint: fp,
			Reason:      accepted[fp],
		})
	}

	if len(rew.Mutations) == 0 {
		return pkgResult{stale: stale}, nil
	}

	funcRanges := schema.FuncRanges(pkg)
	start := time.Now()

	// Narrow by --mutant IDs after schema assignment. The dispatcher keeps all
	// cases; un-listed IDs simply never get activated.
	mutations := rew.Mutations
	var matched []string
	if cfg.mutants != nil {
		filtered := mutations[:0:0]
		for _, m := range mutations {
			if cfg.mutants.has(m) {
				filtered = append(filtered, m)
			}
			if cfg.mutants.fps[m.Fingerprint] {
				matched = append(matched, m.Fingerprint)
			}
		}
		mutations = filtered
		if len(mutations) == 0 {
			return pkgResult{stale: stale}, nil
		}
	}

	cfg.progress.printf("%s: %d mutants; compiling", pkg.ImportPath, len(mutations))
	overlayPath, cleanOverlay, err := runner.BuildOverlay(rew, pkg.Dir)
	if err != nil {
		return pkgResult{}, &stageError{"rewrite", fmt.Errorf("build overlay: %w", err)}
	}
	defer cleanOverlay()

	binaryPath, cleanBin, err := slotted(ctx, cfg, func() (string, func(), error) {
		return runner.CompileTestBinary(ctx, pkg.ImportPath, overlayPath, pkg.GoVersion, cfg.buildFlags...)
	})
	if err != nil {
		return pkgResult{}, &stageError{"compile", err}
	}
	defer cleanBin()

	// Separate cover-instrumented binary built without overlay; used only
	// for per-test coverage collection. See CompileCoverageBinary for why.
	covBinaryPath, cleanCovBin, err := slotted(ctx, cfg, func() (string, func(), error) {
		return runner.CompileCoverageBinary(ctx, pkg.ImportPath, cfg.buildFlags...)
	})
	if err != nil {
		return pkgResult{}, &stageError{"compile", fmt.Errorf("coverage binary: %w", err)}
	}
	defer cleanCovBin()

	// Mutants run in the real checkout.
	defer guardDir(pkg.Dir, pkg.ImportPath, stderr)()

	cfg.progress.printf("%s: running the tests unmutated", pkg.ImportPath)
	baseline, excluded, err := slotted(ctx, cfg, func() (runner.Baseline, []report.ExcludedTest, error) {
		return establishBaseline(ctx, binaryPath, covBinaryPath, pkg.Dir, cfg)
	})
	if err != nil {
		return pkgResult{}, &stageError{"baseline", err}
	}

	// Collect per-test coverage once; pays off across all mutants. A test
	// that fails on its own cannot be trusted to judge a mutant either.
	covMap, covFailed, err := runner.CollectPerTestCoverage(ctx, covBinaryPath, pkg.Dir, pkg.Dir, baseline.Tests, cfg.setupTimeout(), cfg.slots, cfg.testArgs...)
	if err != nil {
		return pkgResult{}, &stageError{"coverage", err}
	}
	var inventory []string
	for _, name := range baseline.Tests {
		if msg, failed := covFailed[name]; failed {
			excluded = append(excluded, report.ExcludedTest{
				Test:   name,
				Reason: "fails when run on its own (it depends on another test, is flaky, or hangs): " + failureMessage(msg),
			})
			continue
		}
		inventory = append(inventory, name)
	}
	for i := range excluded {
		excluded[i].Package = pkg.ImportPath
	}
	if len(inventory) == 0 && len(covFailed) > 0 {
		return pkgResult{}, &stageError{"coverage", errors.New("no test passes when run on its own")}
	}

	suites := []suite{{dir: pkg.Dir, bin: binaryPath, baseline: baseline, covMap: covMap, own: true}}
	deps, depExcluded, cleanDeps := dependentSuites(ctx, pkg, overlayPath, cfg, stderr)
	defer cleanDeps()
	suites = append(suites, deps...)
	excluded = append(excluded, depExcluded...)

	// Per-mutant covering tests are read out up front so the parallel loop
	// below does not touch the coverage maps concurrently and so the
	// `relevant` set (used to narrow the reported inventory) is complete
	// before any goroutine starts. Indexed slices preserve schema-ID order
	// across the parallel phase, keeping JSON output byte-identical to
	// --jobs=1.
	//
	// Every covering test runs against the mutant, in scoped runs too:
	// dropping a test by its hit count changes the verdict, since the one
	// test that asserts on a line may well execute it only once.
	perMut := make([][][]string, len(mutations)) // mutation → suite → tests
	covering := make([][]string, len(mutations)) // mutation → result names
	relevant := make(map[string]bool)
	for i, mut := range mutations {
		perMut[i] = make([][]string, len(suites))
		for j, s := range suites {
			tests := s.coveringTests(mut)
			if s.own && mut.PackageInit && len(tests) == 0 {
				// Package-level var initializers run in every test
				// process but sit outside every cover block, so any test
				// may kill them.
				tests = append(tests, inventory...)
			}
			if s.own {
				for _, name := range tests {
					relevant[name] = true
				}
			}
			perMut[i][j] = tests
			for _, name := range tests {
				covering[i] = append(covering[i], s.resultName(name))
			}
		}
	}

	// Kills from earlier runs are reused only for a verdict alone: a kill
	// matrix needs every killer, and --verify-kills asks for a fresh run.
	kills := cfg.kills
	if cfg.moduleStamp == "" {
		kills = nil
	}
	keys := kills.mutantKeys(mutations, cfg.moduleStamp, pkg.ImportPath, fmt.Sprint(cfg.buildFlags), fmt.Sprint(cfg.testArgs))
	testHash := make(map[string]string) // result name → test source hash
	var stored map[string]killEntry
	if kills != nil {
		for _, s := range suites {
			for name, h := range testHashes(s.dir) {
				testHash[s.resultName(name)] = h
			}
		}
		if cfg.failFast && !cfg.verifyKills {
			stored = kills.load(pkg.ImportPath)
		}
	}

	results := make([]mutation.Result, len(mutations))
	covered, reused := 0, 0
	for i, tests := range covering {
		if len(tests) == 0 {
			continue
		}
		covered++
		if killer := reusedKiller(stored[mutations[i].Fingerprint], keys[i], testHash, tests); killer != "" {
			results[i] = mutation.Result{
				Mutation:      mutations[i],
				Status:        mutation.StatusKilled,
				KillingTests:  []string{killer},
				CoveringTests: tests,
				Cached:        true,
			}
			reused++
		}
	}
	if reused > 0 {
		cfg.progress.printf("%s: reusing %d kills from earlier runs", pkg.ImportPath, reused)
	}
	cfg.progress.printf("%s: testing %d covered mutants (%d not covered) with %d test(s)", pkg.ImportPath, covered-reused, len(mutations)-covered, len(inventory))
	var done atomic.Int64
	toRun := covered - reused
	reportEvery := max(toRun/10, 1)
	lastReport := time.Now()
	var reportMu sync.Mutex

	// The mutants with the longest expected run start first, so the last
	// worker slots to free up are not held by the slowest mutants.
	var order []int
	cost := make([]time.Duration, len(mutations))
	for i, mut := range mutations {
		if len(covering[i]) == 0 {
			// No test covers this line — the mutant cannot be killed,
			// so skip the run and mark it not-covered.
			results[i] = mutation.Result{
				Mutation: mut,
				Status:   mutation.StatusNotCovered,
			}
			continue
		}
		if results[i].Cached {
			continue
		}
		order = append(order, i)
		cost[i] = expectedCost(suites, perMut[i])
	}
	longestFirst(order, cost)

	g, gctx := errgroup.WithContext(ctx)
	var acquireErr error
	for _, i := range order {
		i, mut := i, mutations[i]
		if acquireErr = cfg.slots.Acquire(gctx, 1); acquireErr != nil {
			break
		}
		g.Go(func() error {
			defer cfg.slots.Release(1)
			status, killingTests, dur, err := runMutantSuites(gctx, suites, mut.ID, perMut[i], 1, cfg)
			if err != nil {
				return fmt.Errorf("run mutant %d: %w", mut.ID, err)
			}
			results[i] = mutation.Result{
				Mutation:      mut,
				Status:        status,
				KillingTests:  killingTests,
				CoveringTests: covering[i],
				Duration:      dur,
			}
			n := int(done.Add(1))
			reportMu.Lock()
			if n < toRun && (n%reportEvery == 0 || time.Since(lastReport) > 30*time.Second) {
				lastReport = time.Now()
				cfg.progress.printf("%s: %d/%d mutants tested", pkg.ImportPath, n, toRun)
			}
			reportMu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return pkgResult{}, &stageError{"mutants", err}
	}
	if acquireErr != nil {
		return pkgResult{}, &stageError{"mutants", acquireErr}
	}

	// Re-run uncertain verdicts one at a time, away from the contention of
	// the parallel loop: each holds every worker slot, so nothing else, in
	// this package or another, runs beside it.
	rerun := func(i int, r mutation.Result, timeoutScale float64) (mutation.Result, error) {
		if err := cfg.slots.Acquire(ctx, int64(cfg.jobs)); err != nil {
			return r, err
		}
		status, killingTests, dur, err := runMutantSuites(ctx, suites, r.Mutation.ID, perMut[i], timeoutScale, cfg)
		cfg.slots.Release(int64(cfg.jobs))
		if err != nil {
			return r, fmt.Errorf("re-run mutant %d: %w", r.Mutation.ID, err)
		}
		r.Status, r.KillingTests, r.Duration = status, killingTests, dur
		return r, nil
	}
	if err := confirmVerdicts(results, cfg.verifyKills, rerun); err != nil {
		return pkgResult{}, &stageError{"mutants", err}
	}
	cfg.progress.printf("%s: done in %s", pkg.ImportPath, time.Since(start).Round(100*time.Millisecond))
	kills.update(pkg.ImportPath, killUpdates(results, keys, testHash))

	// When a scope is active (filter or --mutant), drop tests from the reported
	// inventory that don't cover any kept mutation. This keeps "zero_kill_tests"
	// and the redundant-group analysis focused on tests an LLM could actually
	// expect to kill the targeted mutants.
	if cfg.filter != nil || cfg.mutants != nil {
		narrowed := inventory[:0:0]
		for _, name := range inventory {
			if relevant[name] {
				narrowed = append(narrowed, name)
			}
		}
		inventory = narrowed
	}

	return pkgResult{results: results, inventory: inventory, excluded: excluded, funcRanges: funcRanges, ignored: rew.Ignored, stale: stale, matched: matched}, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	const usage = "usage: kanly guide | kanly init --agent=claude | kanly hook stop | kanly show <pkg> <mutant>... | kanly test [--in=<pkg>] <pkg> <mutant> [go test flags]\n       kanly [--version] [--format=text|json|llm|sarif] [--timeout=<dur>] [--timeout-factor=3] [--diff [--diff-base=<ref>]] [--tests=<regex>] [--mutant=<id-or-fp-list>] [--jobs=N] [--verify-kills] [--kill-matrix] [--cross-package] [--no-cache] [--llm-limit=30] [--min-score=N] [--quiet] [--tags=a,b] [--short] [--operators=...] [--skip-operators=...] [--exclude=...] <pattern>[:<func-list>]...\n"

	fs := flag.NewFlagSet("kanly", flag.ContinueOnError)
	fs.SetOutput(stderr)
	versionFlag := fs.Bool("version", false, "print version and exit")
	formatFlag := fs.String("format", "text", "output format: text|json|llm|sarif")
	minScoreFlag := fs.Float64("min-score", 0, "exit 3 when the score including not-covered mutants is below this percentage")
	quietFlag := fs.Bool("quiet", false, "print no progress to stderr; errors and warnings are still printed")
	timeoutFlag := fs.Duration("timeout", 0, "fixed per-mutant test timeout; 0 derives each mutant's timeout from the baseline run time of the tests it runs")
	timeoutFactorFlag := fs.Float64("timeout-factor", defaultTimeoutFactor, "for derived timeouts, the multiple of the tests' baseline run time a mutant may take")
	diffFlag := fs.Bool("diff", false, "only mutate lines changed since --diff-base")
	diffBaseFlag := fs.String("diff-base", "HEAD", "git ref to diff against when --diff is set")
	testsFlag := fs.String("tests", "", "regex narrowing the test inventory used by baseline and per-test coverage")
	operatorsFlag := fs.String("operators", "", "comma-separated operators to run (default all)")
	skipOperatorsFlag := fs.String("skip-operators", "", "comma-separated operators not to run")
	tagsFlag := fs.String("tags", "", "comma-separated build tags, as for go build -tags")
	shortFlag := fs.Bool("short", false, "run tests with -test.short")
	excludeFlag := fs.String("exclude", "", "comma-separated file globs not to mutate, matched against the path relative to the working directory, the file name, or a directory prefix")
	mutantFlag := fs.String("mutant", "", "comma-separated mutant IDs or fingerprints to re-run; others are skipped")
	jobsFlag := fs.Int("jobs", runtime.NumCPU(), "worker slots shared by every compile and test process across packages, and the most packages in progress at once (1 = sequential)")
	verifyKillsFlag := fs.Bool("verify-kills", false, "re-run every killed mutant alone and count kills that do not reproduce as survived")
	killMatrixFlag := fs.Bool("kill-matrix", false, "run every covering test against each killed mutant, for the per-test views (tests, zero_kill_tests, redundant_test_groups); slower")
	// Stopping at the first failing test is the default; the flag is kept
	// so existing scripts and hooks keep working.
	failFastFlag := fs.Bool("fail-fast", false, "stop testing each mutant at its first failing test (the default; see --kill-matrix)")
	llmLimitFlag := fs.Int("llm-limit", defaultLLMLimit, "most mutants the llm brief lists, those in the functions with the most first; 0 lists all")
	crossPackageFlag := fs.Bool("cross-package", false, "also run the tests of the module's other packages that import the mutated package")
	noCacheFlag := fs.Bool("no-cache", false, "run every mutant, and with --cross-package set up every dependent's tests, instead of reusing results of earlier runs")

	if err := fs.Parse(args); err != nil {
		fmt.Fprint(stderr, usage)
		return 2
	}

	if *versionFlag {
		fmt.Fprintf(stdout, "kanly %s\n", resolveVersion())
		return 0
	}

	switch *formatFlag {
	case "text", "json", "llm", "sarif":
	default:
		fmt.Fprintf(stderr, "--format: must be text, json, llm or sarif, got %q\n", *formatFlag)
		return 2
	}
	if *minScoreFlag < 0 || *minScoreFlag > 100 {
		fmt.Fprintf(stderr, "--min-score: must be between 0 and 100, got %v\n", *minScoreFlag)
		return 2
	}
	// Packages run concurrently, so every write to stderr goes through one
	// lock and lines from different packages never interleave.
	stderr = &syncWriter{w: stderr}
	prog := &progress{w: stderr}
	if *quietFlag {
		prog.w = nil
	}

	if *timeoutFlag < 0 {
		fmt.Fprintf(stderr, "--timeout: must be >= 0, got %v\n", *timeoutFlag)
		return 2
	}
	if *timeoutFactorFlag < 1 {
		fmt.Fprintf(stderr, "--timeout-factor: must be >= 1, got %v\n", *timeoutFactorFlag)
		return 2
	}

	if *llmLimitFlag < 0 {
		fmt.Fprintf(stderr, "--llm-limit: must be >= 0, got %d\n", *llmLimitFlag)
		return 2
	}

	if *jobsFlag < 1 {
		fmt.Fprintf(stderr, "--jobs: must be >= 1, got %d\n", *jobsFlag)
		return 2
	}

	if *failFastFlag && *killMatrixFlag {
		fmt.Fprintln(stderr, "--fail-fast and --kill-matrix are mutually exclusive")
		return 2
	}

	excludePred, err := excludeFilter(*excludeFlag)
	if err != nil {
		fmt.Fprintf(stderr, "--exclude: %v\n", err)
		return 2
	}

	mutants, err := parseMutants(*mutantFlag)
	if err != nil {
		fmt.Fprintf(stderr, "--mutant: %v\n", err)
		return 2
	}

	if *testsFlag != "" {
		if _, err := regexp.Compile(*testsFlag); err != nil {
			fmt.Fprintf(stderr, "--tests: invalid regex: %v\n", err)
			return 2
		}
	}

	specs, err := selector.Parse(fs.Args())
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		fmt.Fprint(stderr, usage)
		return 2
	}

	var diffPred func(file string, line int) bool

	if *diffFlag {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "getwd: %v\n", err)
			return 1
		}
		d, err := diff.Compute(cwd, *diffBaseFlag)
		if err != nil {
			fmt.Fprintf(stderr, "compute diff: %v\n", err)
			return 1
		}
		diffPred = d.Includes
		if len(d.Files()) == 0 {
			return writeReport(stdout, stderr, *formatFlag, report.Build(nil, nil), report.LLMSource{})
		}
		if len(specs) == 0 {
			for _, p := range d.Patterns(cwd) {
				specs = append(specs, selector.Spec{Pattern: p})
			}
			if len(specs) == 0 {
				return writeReport(stdout, stderr, *formatFlag, report.Build(nil, nil), report.LLMSource{})
			}
		}
	} else if len(specs) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	ops := mutation.Operators()
	if len(ops) == 0 {
		fmt.Fprintf(stderr, "no operators registered\n")
		return 1
	}
	var buildFlags, testArgs []string
	if *tagsFlag != "" {
		buildFlags = append(buildFlags, "-tags="+*tagsFlag)
	}
	if *shortFlag {
		testArgs = append(testArgs, "-test.short")
	}

	operatorSet, err := selectOperators(ops, *operatorsFlag, *skipOperatorsFlag)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 2
	}

	ctx := context.Background()
	var allResults []mutation.Result
	testInventory := make(map[string][]string)
	funcRangesByPkg := make(map[string]map[string]mutation.FuncRange)

	// A package that fails is recorded and the run moves on, so one broken
	// package cannot discard its siblings' results.
	var pkgErrors []report.PackageError
	var excludedTests []report.ExcludedTest
	var staleIgnores []report.StaleIgnore
	matchedFPs := make(map[string]bool)
	ignored := 0
	var cache *setupCache
	var kills *killCache
	if !*noCacheFlag {
		kills = openKillCache()
		if *crossPackageFlag {
			cache = openSetupCache()
		}
	}
	// The module is stamped for the caches, and its dependents listed, once
	// per module on first use: before any of its tests run, so files tests
	// leave behind do not change the stamp.
	stamps := make(map[string]string)
	stampOf := func(pkg *source.Package) string {
		if (cache == nil && kills == nil) || pkg.ModuleDir == "" {
			return ""
		}
		stamp, ok := stamps[pkg.ModuleDir]
		if !ok {
			var err error
			stamp, err = moduleStamp(pkg.ModuleDir)
			if err != nil {
				fmt.Fprintf(stderr, "warning: not reusing results of earlier runs: %v\n", err)
			}
			stamps[pkg.ModuleDir] = stamp
		}
		return stamp
	}
	dependentsByModule := make(map[string]map[string][]runner.Dependent)
	dependentsOf := func(pkg *source.Package) []runner.Dependent {
		if !*crossPackageFlag || pkg.ModuleDir == "" {
			return nil
		}
		deps, ok := dependentsByModule[pkg.ModuleDir]
		if !ok {
			var err error
			deps, err = runner.TestDependents(ctx, pkg.ModuleDir, buildFlags...)
			if err != nil {
				fmt.Fprintf(stderr, "warning: --cross-package: listing the packages of %s: %v\n", pkg.ModuleDir, err)
			}
			dependentsByModule[pkg.ModuleDir] = deps
		}
		return deps[pkg.ImportPath]
	}
	// A failure is reported on stderr as it happens and recorded for the
	// report once every package is done.
	announceFailure := func(pkg string, err error) {
		stage, msg := failureStage(err)
		first, rest, multiline := strings.Cut(msg.Error(), "\n")
		if multiline && strings.TrimSpace(rest) != "" {
			first += " … (details in the report)"
		}
		fmt.Fprintf(stderr, "error in %s (%s): %s\n", pkg, stage, first)
	}
	recordFailure := func(pkg string, err error) {
		stage, msg := failureStage(err)
		pkgErrors = append(pkgErrors, report.PackageError{Package: pkg, Stage: stage, Error: msg.Error()})
	}

	// Packages are loaded, checked and configured in order; then their
	// pipelines run concurrently, sharing the worker slots, and their results
	// are put together in that same order, so the report does not depend on
	// which package finished first.
	type job struct {
		pkg *source.Package
		cfg runConfig
		res pkgResult
		err error // set before the run for a package that failed to load
	}
	var jobs []*job
	slots := semaphore.NewWeighted(int64(*jobsFlag))
	for _, spec := range specs {
		pkgs, loadErrs, err := source.LoadAllTags("", *tagsFlag, spec.Pattern)
		if err != nil {
			fmt.Fprintf(stderr, "error loading packages for %q: %v\n", spec.Pattern, err)
			return 1
		}
		if len(pkgs) == 0 && len(loadErrs) == 0 {
			fmt.Fprintf(stderr, "no packages matched %q\n", spec.Pattern)
			return 1
		}
		if n := len(pkgs) + len(loadErrs); len(spec.Funcs) > 0 && n != 1 {
			fmt.Fprintf(stderr, "selector %q with function filter must resolve to one package; got %d\n", spec.Pattern, n)
			return 1
		}
		for _, le := range loadErrs {
			err := &stageError{"load", le.Err}
			announceFailure(le.ImportPath, err)
			jobs = append(jobs, &job{pkg: &source.Package{ImportPath: le.ImportPath}, err: err})
		}

		for _, pkg := range pkgs {
			testFiles, err := filepath.Glob(filepath.Join(pkg.Dir, "*_test.go"))
			if err != nil || len(testFiles) == 0 {
				prog.printf("skip %s: no test files", pkg.ImportPath)
				continue
			}

			if len(spec.Funcs) > 0 {
				if err := validateFuncSelectors(pkg, spec.Funcs); err != nil {
					fmt.Fprintf(stderr, "%v\n", err)
					return 1
				}
			}

			stamp := stampOf(pkg)
			deps := dependentsOf(pkg)
			jobs = append(jobs, &job{pkg: pkg, cfg: runConfig{
				filter:        composeFilter(diffPred, excludePred, spec.Funcs),
				timeout:       *timeoutFlag,
				timeoutFactor: *timeoutFactorFlag,
				testsRegex:    *testsFlag,
				mutants:       mutants,
				operators:     operatorSet,
				buildFlags:    buildFlags,
				testArgs:      testArgs,
				progress:      prog,
				jobs:          *jobsFlag,
				slots:         slots,
				verifyKills:   *verifyKillsFlag,
				failFast:      !*killMatrixFlag,
				dependents:    deps,
				cache:         cache,
				kills:         kills,
				moduleStamp:   stamp,
			}})
		}
	}

	// At most --jobs pipelines are in flight, started in order: each keeps
	// its test binaries on disk until it is done, and that many already
	// keep every worker slot busy while one of them compiles or waits.
	// A package's tests run in its own directory and, with --cross-package,
	// in its dependents'. Two pipelines never run tests in, or guard, the
	// same directory at once: each holds a lock on every directory it uses.
	pipelines := semaphore.NewWeighted(int64(*jobsFlag))
	locks := newDirLocks()
	var wg sync.WaitGroup
	for _, j := range jobs {
		if j.err != nil {
			continue
		}
		dirs := []string{j.pkg.Dir}
		for _, dep := range j.cfg.dependents {
			dirs = append(dirs, dep.Dir)
		}
		if err := pipelines.Acquire(ctx, 1); err != nil {
			j.err = err
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer pipelines.Release(1)
			unlock := locks.lock(dirs)
			defer unlock()
			j.res, j.err = runPackage(ctx, j.pkg, ops, j.cfg, stderr)
			if j.err != nil {
				announceFailure(j.pkg.ImportPath, j.err)
			}
		}()
	}
	wg.Wait()

	for _, j := range jobs {
		pkg := j.pkg
		res, err := j.res, j.err
		if err != nil {
			recordFailure(pkg.ImportPath, err)
			continue
		}
		staleIgnores = append(staleIgnores, res.stale...)
		for _, fp := range res.matched {
			matchedFPs[fp] = true
		}
		if len(res.results) == 0 {
			prog.printf("skip %s: no mutations", pkg.ImportPath)
			continue
		}
		excludedTests = append(excludedTests, res.excluded...)
		ignored += res.ignored
		allResults = append(allResults, res.results...)
		testInventory[pkg.ImportPath] = res.inventory
		funcRangesByPkg[pkg.ImportPath] = res.funcRanges
	}

	rep := report.Build(allResults, testInventory)
	rep.Scope = describeScope(specs, *diffFlag, *diffBaseFlag, *testsFlag, *mutantFlag)
	for _, f := range []struct{ name, value string }{
		{"operators", *operatorsFlag}, {"skip-operators", *skipOperatorsFlag}, {"exclude", *excludeFlag}, {"tags", *tagsFlag},
	} {
		if f.value != "" {
			rep.Scope = strings.TrimSpace(rep.Scope + " --" + f.name + "=" + f.value)
		}
	}
	if *shortFlag {
		rep.Scope = strings.TrimSpace(rep.Scope + " --short")
	}
	if *crossPackageFlag {
		rep.Scope = strings.TrimSpace(rep.Scope + " --cross-package")
		rep.CrossPackage = true
	}
	if !*killMatrixFlag {
		// Each kill set holds only the first failing test, so the
		// per-test views would call tests that kill plenty useless.
		rep.FailFast = true
		rep.Tests, rep.ZeroKillTests, rep.RedundantTestGroups = []mutation.TestStats{}, []string{}, [][]string{}
	}
	rep.Summary.Ignored = ignored
	rep.Errors = pkgErrors
	rep.ExcludedTests = dedupeExcluded(excludedTests)
	rep.StaleIgnores = staleIgnores
	if mutants != nil {
		for fp := range mutants.fps {
			if !matchedFPs[fp] {
				rep.UnmatchedMutants = append(rep.UnmatchedMutants, fp)
			}
		}
		sort.Strings(rep.UnmatchedMutants)
		if len(rep.UnmatchedMutants) > 0 {
			fmt.Fprintf(stderr, "kanly: --mutant: no mutant in scope has fingerprint %s; its line changed or it is accepted in %s\n", strings.Join(rep.UnmatchedMutants, ", "), schema.IgnoreFileName)
		}
	}
	llmSrc := report.LLMSource{FuncRanges: funcRangesByPkg, Limit: *llmLimitFlag}
	if code := writeReport(stdout, stderr, *formatFlag, rep, llmSrc); code != 0 {
		return code
	}
	if len(pkgErrors) > 0 {
		// The report is complete for every other package, but the run is
		// not: never let a partial result pass as success.
		return 1
	}
	if got := rep.Summary.ScoreWithUncovered * 100; rep.Summary.Total > 0 && got < *minScoreFlag {
		fmt.Fprintf(stderr, "kanly: score including not-covered mutants is %.1f%%, below --min-score=%v\n", got, *minScoreFlag)
		return 3
	}
	return 0
}

// dedupeExcluded drops repeats of an excluded test: with --cross-package
// a dependent's failing test is found once per package it imports.
func dedupeExcluded(tests []report.ExcludedTest) []report.ExcludedTest {
	seen := make(map[report.ExcludedTest]bool)
	out := tests[:0:0]
	for _, t := range tests {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// unionHits returns the tests in a or b, each once.
func unionHits(a, b []runner.TestHit) []runner.TestHit {
	seen := make(map[string]bool)
	var out []runner.TestHit
	for _, h := range append(a[:len(a):len(a)], b...) {
		if !seen[h.Name] {
			seen[h.Name] = true
			out = append(out, h)
		}
	}
	return out
}

// composeFilter combines an optional diff (file, line) predicate, an
// optional file-exclusion predicate and an optional function-name selector
// list. Returns nil when none is active so the rewriter takes the fast
// no-filter path.
func composeFilter(diffPred func(file string, line int) bool, exclude func(file string) bool, funcs []string) func(string, int, string) bool {
	if diffPred == nil && exclude == nil && len(funcs) == 0 {
		return nil
	}
	return func(file string, line int, funcName string) bool {
		if diffPred != nil && !diffPred(file, line) {
			return false
		}
		if exclude != nil && exclude(file) {
			return false
		}
		if len(funcs) > 0 && !selector.AnyMatch(funcs, funcName) {
			return false
		}
		return true
	}
}

// selectOperators returns the operator names to run given --operators and
// --skip-operators, or nil for all of them. Unknown names are an error.
func selectOperators(ops []mutation.Operator, only, skip string) (map[string]bool, error) {
	if only == "" && skip == "" {
		return nil, nil
	}
	known := make(map[string]bool, len(ops))
	var names []string
	for _, op := range ops {
		known[op.Name()] = true
		names = append(names, op.Name())
	}
	parse := func(flagName, list string) (map[string]bool, error) {
		set := make(map[string]bool)
		for _, n := range strings.Split(list, ",") {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			if !known[n] {
				return nil, fmt.Errorf("--%s: unknown operator %q; known: %s", flagName, n, strings.Join(names, ", "))
			}
			set[n] = true
		}
		return set, nil
	}
	selected := known
	if only != "" {
		set, err := parse("operators", only)
		if err != nil {
			return nil, err
		}
		selected = set
	}
	skipped, err := parse("skip-operators", skip)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool)
	for n := range selected {
		if !skipped[n] {
			out[n] = true
		}
	}
	if len(out) == 0 {
		return nil, errors.New("--operators/--skip-operators leave no operator to run")
	}
	return out, nil
}

// excludeFilter parses --exclude into a predicate reporting whether an
// absolute file path is excluded, or nil when the list is empty. A pattern
// matches the path relative to the working directory, the file's base name,
// or (without glob characters) a directory prefix of the relative path.
func excludeFilter(list string) (func(file string) bool, error) {
	var patterns []string
	for _, p := range strings.Split(list, ",") {
		p = strings.TrimSuffix(filepath.ToSlash(strings.TrimSpace(p)), "/")
		p = strings.TrimPrefix(p, "./")
		if p == "" {
			continue
		}
		if _, err := path.Match(p, ""); err != nil {
			return nil, fmt.Errorf("bad pattern %q: %w", p, err)
		}
		patterns = append(patterns, p)
	}
	if len(patterns) == 0 {
		return nil, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return func(file string) bool {
		rel := file
		if r, err := filepath.Rel(cwd, file); err == nil {
			rel = r
		}
		rel = filepath.ToSlash(rel)
		base := path.Base(rel)
		for _, p := range patterns {
			if m, _ := path.Match(p, rel); m {
				return true
			}
			if m, _ := path.Match(p, base); m {
				return true
			}
			if strings.HasPrefix(rel, p+"/") {
				return true
			}
		}
		return false
	}, nil
}

// validateFuncSelectors checks that every user-supplied function entry matches
// at least one canonical FuncDecl name in pkg, and otherwise returns an error
// with the top-3 nearest names as suggestions.
func validateFuncSelectors(pkg *source.Package, funcs []string) error {
	names := schema.FuncNames(pkg)
	for _, entry := range funcs {
		matched := false
		for _, n := range names {
			if selector.MatchFunc(entry, n) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("no function matches %q in %s; available: %s",
				entry, pkg.ImportPath, suggestNames(entry, names, 3))
		}
	}
	return nil
}

// suggestNames returns up to maxN names from names, ordered by Levenshtein
// distance to entry, comma-joined.
func suggestNames(entry string, names []string, maxN int) string {
	if len(names) == 0 {
		return "<no top-level funcs in package>"
	}
	type scored struct {
		name string
		dist int
	}
	scoredNames := make([]scored, 0, len(names))
	for _, n := range names {
		scoredNames = append(scoredNames, scored{name: n, dist: levenshtein(entry, n)})
	}
	sort.Slice(scoredNames, func(i, j int) bool {
		if scoredNames[i].dist != scoredNames[j].dist {
			return scoredNames[i].dist < scoredNames[j].dist
		}
		return scoredNames[i].name < scoredNames[j].name
	})
	if len(scoredNames) > maxN {
		scoredNames = scoredNames[:maxN]
	}
	out := make([]string, len(scoredNames))
	for i, s := range scoredNames {
		out[i] = s.name
	}
	return strings.Join(out, ", ")
}

// levenshtein returns the edit distance between a and b.
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// mutantSet is the --mutant selection: schema IDs and fingerprints.
type mutantSet struct {
	ids map[int]bool
	fps map[string]bool
}

func (s *mutantSet) has(m mutation.Mutation) bool {
	return s.ids[m.ID] || m.Fingerprint != "" && s.fps[m.Fingerprint]
}

// parseMutants parses a --mutant list such as "7,12,5e0d8f5590d5": each
// entry is a fingerprint (12 hex digits) or else a schema ID. An empty
// string returns nil (the "no filter" sentinel). Invalid entries error out.
func parseMutants(s string) (*mutantSet, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	out := &mutantSet{ids: make(map[int]bool), fps: make(map[string]bool)}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty id in list %q", s)
		}
		if schema.IsFingerprint(part) {
			out.fps[part] = true
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid id %q: want a mutant ID or a 12-hex-digit fingerprint", part)
		}
		if id < 1 {
			return nil, fmt.Errorf("invalid id %d: must be >= 1", id)
		}
		out.ids[id] = true
	}
	return out, nil
}

// describeScope renders a one-line summary of the active filters, included in
// the report so LLM consumers see exactly what was tested.
func describeScope(specs []selector.Spec, diff bool, diffBase, tests, mutants string) string {
	var parts []string
	for _, s := range specs {
		if len(s.Funcs) > 0 {
			parts = append(parts, s.Pattern+":"+strings.Join(s.Funcs, ","))
		} else {
			parts = append(parts, s.Pattern)
		}
	}
	if diff {
		parts = append(parts, "--diff="+diffBase)
	}
	if tests != "" {
		parts = append(parts, "--tests="+tests)
	}
	if mutants != "" {
		parts = append(parts, "--mutant="+mutants)
	}
	return strings.Join(parts, " ")
}

func writeReport(stdout, stderr io.Writer, format string, r report.Report, llmSrc report.LLMSource) int {
	switch format {
	case "json":
		if err := report.WriteJSON(stdout, r); err != nil {
			fmt.Fprintf(stderr, "write JSON: %v\n", err)
			return 1
		}
	case "sarif":
		if err := report.WriteSARIF(stdout, r, resolveVersion()); err != nil {
			fmt.Fprintf(stderr, "write sarif: %v\n", err)
			return 1
		}
	case "llm":
		if err := report.WriteLLM(stdout, r, llmSrc); err != nil {
			fmt.Fprintf(stderr, "write llm: %v\n", err)
			return 1
		}
	default:
		if err := report.WriteText(stdout, r); err != nil {
			fmt.Fprintf(stderr, "write text: %v\n", err)
			return 1
		}
	}
	return 0
}
