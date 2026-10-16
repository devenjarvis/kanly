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
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

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
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
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
	timeout       time.Duration   // fixed per-mutant timeout; 0 derives it from the baseline
	timeoutFactor float64         // multiplier on baseline test time for derived timeouts
	testsRegex    string          // if non-empty, narrows baseline + per-test coverage
	mutantIDs     map[int]bool    // if non-nil, only these schema-assigned IDs run
	operators     map[string]bool // if non-nil, only these operators' mutants run
	jobs          int             // worker pool size for per-test coverage and the mutant loop; >= 1
	verifyKills   bool            // re-run every killed mutant alone to confirm the kill
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
		b, err := runner.RunBaseline(ctx, schemaBin, dir, cfg.setupTimeout(), cfg.testsRegex, skip)
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
		origFailed, origErr := runner.RunTests(ctx, origBin, dir, cfg.setupTimeout(), tf.Failed)
		schemaFailed, schemaErr := runner.RunTests(ctx, schemaBin, dir, cfg.setupTimeout(), tf.Failed)
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
	if _, origErr := runner.RunTests(ctx, origBin, dir, cfg.setupTimeout(), nil); origErr == nil {
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
func confirmVerdicts(results []mutation.Result, verifyKills bool, rerun func(r mutation.Result, timeoutScale float64) (mutation.Result, error)) error {
	for i, r := range results {
		switch {
		case r.Status == mutation.StatusTimeout:
			again, err := rerun(r, confirmTimeoutScale)
			if err != nil {
				return err
			}
			if again.Status != mutation.StatusTimeout {
				again.Note = fmt.Sprintf("timed out in the parallel run; %s when re-run alone", again.Status)
			}
			results[i] = again
		case r.Status == mutation.StatusKilled && verifyKills:
			again, err := rerun(r, 1)
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

// pkgResult is what one package's pipeline contributes to the report.
type pkgResult struct {
	results    []mutation.Result
	ignored    int                           // mutants suppressed by //kanly:ignore
	inventory  []string                      // tests reported for the package
	excluded   []report.ExcludedTest         // tests left out because they fail unmutated
	funcRanges map[string]mutation.FuncRange // canonical func name → source range
}

func runPackage(ctx context.Context, pkg *source.Package, ops []mutation.Operator, cfg runConfig, stderr io.Writer) (pkgResult, error) {
	rew, err := schema.RewriteWith(pkg, ops, schema.Options{Filter: cfg.filter, Operators: cfg.operators})
	if err != nil {
		return pkgResult{}, &stageError{"rewrite", err}
	}

	if len(rew.Mutations) == 0 {
		return pkgResult{}, nil
	}

	funcRanges := schema.FuncRanges(pkg)

	// Narrow by --mutant IDs after schema assignment. The dispatcher keeps all
	// cases; un-listed IDs simply never get activated.
	mutations := rew.Mutations
	if cfg.mutantIDs != nil {
		filtered := mutations[:0:0]
		for _, m := range mutations {
			if cfg.mutantIDs[m.ID] {
				filtered = append(filtered, m)
			}
		}
		mutations = filtered
		if len(mutations) == 0 {
			return pkgResult{}, nil
		}
	}

	overlayPath, cleanOverlay, err := runner.BuildOverlay(rew, pkg.Dir)
	if err != nil {
		return pkgResult{}, &stageError{"rewrite", fmt.Errorf("build overlay: %w", err)}
	}
	defer cleanOverlay()

	binaryPath, cleanBin, err := runner.CompileTestBinary(ctx, pkg.ImportPath, overlayPath, pkg.GoVersion)
	if err != nil {
		return pkgResult{}, &stageError{"compile", err}
	}
	defer cleanBin()

	// Separate cover-instrumented binary built without overlay; used only
	// for per-test coverage collection. See CompileCoverageBinary for why.
	covBinaryPath, cleanCovBin, err := runner.CompileCoverageBinary(ctx, pkg.ImportPath)
	if err != nil {
		return pkgResult{}, &stageError{"compile", fmt.Errorf("coverage binary: %w", err)}
	}
	defer cleanCovBin()

	// Mutants run in the real checkout. Undo what they leave behind in the
	// package directory, and warn about what cannot be undone.
	if snap, err := runner.SnapshotDir(pkg.Dir); err == nil {
		defer func() {
			removed, changed, err := snap.Restore()
			if err != nil {
				fmt.Fprintf(stderr, "warning: %s: checking for files changed by tests: %v\n", pkg.ImportPath, err)
			}
			if len(removed) > 0 {
				fmt.Fprintf(stderr, "%s: removed %d file(s) that tests created: %s\n", pkg.ImportPath, len(removed), strings.Join(displayPaths(removed), ", "))
			}
			if len(changed) > 0 {
				fmt.Fprintf(stderr, "warning: %s: tests modified or deleted %d file(s); check them before committing: %s\n", pkg.ImportPath, len(changed), strings.Join(displayPaths(changed), ", "))
			}
		}()
	}

	baseline, excluded, err := establishBaseline(ctx, binaryPath, covBinaryPath, pkg.Dir, cfg)
	if err != nil {
		return pkgResult{}, &stageError{"baseline", err}
	}

	// Collect per-test coverage once; pays off across all mutants. A test
	// that fails on its own cannot be trusted to judge a mutant either.
	covMap, covFailed, err := runner.CollectPerTestCoverage(ctx, covBinaryPath, pkg.Dir, baseline.Tests, cfg.setupTimeout(), cfg.jobs)
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
	if len(inventory) == 0 {
		return pkgResult{}, &stageError{"coverage", errors.New("no test passes when run on its own")}
	}

	// Per-mutant covering tests are read out up front so the parallel loop
	// below does not touch covMap concurrently and so the `relevant` set
	// (used to narrow the reported inventory) is complete before any
	// goroutine starts. Indexed slices preserve schema-ID order across the
	// parallel phase, keeping JSON output byte-identical to --jobs=1.
	//
	// Every covering test runs against the mutant, in scoped runs too:
	// dropping a test by its hit count changes the verdict, since the one
	// test that asserts on a line may well execute it only once.
	perMut := make([][]string, len(mutations))
	relevant := make(map[string]bool)
	for i, mut := range mutations {
		hits := covMap[runner.FileLine{File: mut.File, Line: mut.Line}]
		if mut.PackageInit && len(hits) == 0 {
			// Package-level var initializers run in every test process
			// but sit outside every cover block, so any test may kill them.
			for _, name := range inventory {
				hits = append(hits, runner.TestHit{Name: name})
			}
		}
		for _, th := range hits {
			perMut[i] = append(perMut[i], th.Name)
			relevant[th.Name] = true
		}
	}

	results := make([]mutation.Result, len(mutations))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(cfg.jobs)
	for i, mut := range mutations {
		i, mut := i, mut
		tests := perMut[i]
		if len(tests) == 0 {
			// No test covers this line — the mutant cannot be killed,
			// so skip the run and mark it not-covered.
			results[i] = mutation.Result{
				Mutation: mut,
				Status:   mutation.StatusNotCovered,
			}
			continue
		}
		g.Go(func() error {
			status, killingTests, dur, err := runner.RunMutant(gctx, binaryPath, mut.ID, pkg.Dir, tests, cfg.mutantTimeout(baseline, tests))
			if err != nil {
				return fmt.Errorf("run mutant %d: %w", mut.ID, err)
			}
			results[i] = mutation.Result{
				Mutation:      mut,
				Status:        status,
				KillingTests:  killingTests,
				CoveringTests: tests,
				Duration:      dur,
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return pkgResult{}, &stageError{"mutants", err}
	}

	// Re-run uncertain verdicts one at a time, away from the contention of
	// the parallel loop.
	rerun := func(r mutation.Result, timeoutScale float64) (mutation.Result, error) {
		timeout := time.Duration(timeoutScale * float64(cfg.mutantTimeout(baseline, r.CoveringTests)))
		status, killingTests, dur, err := runner.RunMutant(ctx, binaryPath, r.Mutation.ID, pkg.Dir, r.CoveringTests, timeout)
		if err != nil {
			return r, fmt.Errorf("re-run mutant %d: %w", r.Mutation.ID, err)
		}
		r.Status, r.KillingTests, r.Duration = status, killingTests, dur
		return r, nil
	}
	if err := confirmVerdicts(results, cfg.verifyKills, rerun); err != nil {
		return pkgResult{}, &stageError{"mutants", err}
	}

	// When a scope is active (filter or --mutant), drop tests from the reported
	// inventory that don't cover any kept mutation. This keeps "zero_kill_tests"
	// and the redundant-group analysis focused on tests an LLM could actually
	// expect to kill the targeted mutants.
	if cfg.filter != nil || cfg.mutantIDs != nil {
		narrowed := inventory[:0:0]
		for _, name := range inventory {
			if relevant[name] {
				narrowed = append(narrowed, name)
			}
		}
		inventory = narrowed
	}

	return pkgResult{results: results, inventory: inventory, excluded: excluded, funcRanges: funcRanges, ignored: rew.Ignored}, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	const usage = "usage: kanly [--version] [--format=text|json|llm] [--timeout=<dur>] [--timeout-factor=3] [--diff [--diff-base=<ref>]] [--tests=<regex>] [--mutant=<id-list>] [--jobs=N] [--verify-kills] <pattern>[:<func-list>]...\n"

	fs := flag.NewFlagSet("kanly", flag.ContinueOnError)
	fs.SetOutput(stderr)
	versionFlag := fs.Bool("version", false, "print version and exit")
	formatFlag := fs.String("format", "text", "output format: text|json|llm")
	timeoutFlag := fs.Duration("timeout", 0, "fixed per-mutant test timeout; 0 derives each mutant's timeout from the baseline run time of the tests it runs")
	timeoutFactorFlag := fs.Float64("timeout-factor", defaultTimeoutFactor, "for derived timeouts, the multiple of the tests' baseline run time a mutant may take")
	diffFlag := fs.Bool("diff", false, "only mutate lines changed since --diff-base")
	diffBaseFlag := fs.String("diff-base", "HEAD", "git ref to diff against when --diff is set")
	testsFlag := fs.String("tests", "", "regex narrowing the test inventory used by baseline and per-test coverage")
	operatorsFlag := fs.String("operators", "", "comma-separated operators to run (default all)")
	skipOperatorsFlag := fs.String("skip-operators", "", "comma-separated operators not to run")
	excludeFlag := fs.String("exclude", "", "comma-separated file globs not to mutate, matched against the path relative to the working directory, the file name, or a directory prefix")
	mutantFlag := fs.String("mutant", "", "comma-separated schema-assigned mutant IDs to re-run; others are skipped")
	jobsFlag := fs.Int("jobs", runtime.NumCPU(), "parallel worker processes for per-test coverage and the mutant loop (1 = sequential)")
	verifyKillsFlag := fs.Bool("verify-kills", false, "re-run every killed mutant alone and count kills that do not reproduce as survived")

	if err := fs.Parse(args); err != nil {
		fmt.Fprint(stderr, usage)
		return 2
	}

	if *versionFlag {
		fmt.Fprintf(stdout, "kanly %s\n", resolveVersion())
		return 0
	}

	if *timeoutFlag < 0 {
		fmt.Fprintf(stderr, "--timeout: must be >= 0, got %v\n", *timeoutFlag)
		return 2
	}
	if *timeoutFactorFlag < 1 {
		fmt.Fprintf(stderr, "--timeout-factor: must be >= 1, got %v\n", *timeoutFactorFlag)
		return 2
	}

	if *jobsFlag < 1 {
		fmt.Fprintf(stderr, "--jobs: must be >= 1, got %d\n", *jobsFlag)
		return 2
	}

	excludePred, err := excludeFilter(*excludeFlag)
	if err != nil {
		fmt.Fprintf(stderr, "--exclude: %v\n", err)
		return 2
	}

	mutantIDs, err := parseMutantIDs(*mutantFlag)
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
	ignored := 0
	recordFailure := func(pkg, stage string, err error) {
		msg := err.Error()
		first, rest, multiline := strings.Cut(msg, "\n")
		if multiline && strings.TrimSpace(rest) != "" {
			first += " … (details in the report)"
		}
		fmt.Fprintf(stderr, "error in %s (%s): %s\n", pkg, stage, first)
		pkgErrors = append(pkgErrors, report.PackageError{Package: pkg, Stage: stage, Error: msg})
	}

	for _, spec := range specs {
		pkgs, loadErrs, err := source.LoadAll("", spec.Pattern)
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
			recordFailure(le.ImportPath, "load", le.Err)
		}

		for _, pkg := range pkgs {
			testFiles, err := filepath.Glob(filepath.Join(pkg.Dir, "*_test.go"))
			if err != nil || len(testFiles) == 0 {
				fmt.Fprintf(stderr, "skip %s: no test files\n", pkg.ImportPath)
				continue
			}

			if len(spec.Funcs) > 0 {
				if err := validateFuncSelectors(pkg, spec.Funcs); err != nil {
					fmt.Fprintf(stderr, "%v\n", err)
					return 1
				}
			}

			cfg := runConfig{
				filter:        composeFilter(diffPred, excludePred, spec.Funcs),
				timeout:       *timeoutFlag,
				timeoutFactor: *timeoutFactorFlag,
				testsRegex:    *testsFlag,
				mutantIDs:     mutantIDs,
				operators:     operatorSet,
				jobs:          *jobsFlag,
				verifyKills:   *verifyKillsFlag,
			}

			res, err := runPackage(ctx, pkg, ops, cfg, stderr)
			if err != nil {
				var se *stageError
				if errors.As(err, &se) {
					recordFailure(pkg.ImportPath, se.stage, se.err)
				} else {
					recordFailure(pkg.ImportPath, "mutants", err)
				}
				continue
			}
			if len(res.results) == 0 {
				fmt.Fprintf(stderr, "skip %s: no mutations\n", pkg.ImportPath)
				continue
			}
			excludedTests = append(excludedTests, res.excluded...)
			ignored += res.ignored
			allResults = append(allResults, res.results...)
			testInventory[pkg.ImportPath] = res.inventory
			funcRangesByPkg[pkg.ImportPath] = res.funcRanges
		}
	}

	rep := report.Build(allResults, testInventory)
	rep.Scope = describeScope(specs, *diffFlag, *diffBaseFlag, *testsFlag, *mutantFlag)
	for _, f := range []struct{ name, value string }{
		{"operators", *operatorsFlag}, {"skip-operators", *skipOperatorsFlag}, {"exclude", *excludeFlag},
	} {
		if f.value != "" {
			rep.Scope = strings.TrimSpace(rep.Scope + " --" + f.name + "=" + f.value)
		}
	}
	rep.Summary.Ignored = ignored
	rep.Errors = pkgErrors
	rep.ExcludedTests = excludedTests
	llmSrc := report.LLMSource{FuncRanges: funcRangesByPkg}
	if code := writeReport(stdout, stderr, *formatFlag, rep, llmSrc); code != 0 {
		return code
	}
	if len(pkgErrors) > 0 {
		// The report is complete for every other package, but the run is
		// not: never let a partial result pass as success.
		return 1
	}
	return 0
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

// parseMutantIDs parses "7,12,15" into a set. Empty string returns nil
// (the "no filter" sentinel). Invalid entries error out.
func parseMutantIDs(s string) (map[int]bool, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	out := make(map[int]bool)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty id in list %q", s)
		}
		id, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid id %q: %w", part, err)
		}
		if id < 1 {
			return nil, fmt.Errorf("invalid id %d: must be >= 1", id)
		}
		out[id] = true
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
