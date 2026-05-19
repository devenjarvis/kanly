package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// runConfig collects the per-package knobs threaded from CLI flags into runPackage.
type runConfig struct {
	filter     func(file string, line int, funcName string) bool
	timeout    time.Duration
	testsRegex string       // if non-empty, narrows baseline + per-test coverage
	mutantIDs  map[int]bool // if non-nil, only these schema-assigned IDs run
	jobs       int          // worker pool size for per-test coverage and the mutant loop; >= 1
}

func runPackage(ctx context.Context, pkg *source.Package, ops []mutation.Operator, cfg runConfig, stderr io.Writer) ([]mutation.Result, []string, error) {
	rew, err := schema.Rewrite(pkg, ops, cfg.filter)
	if err != nil {
		return nil, nil, fmt.Errorf("schema rewrite: %w", err)
	}

	if len(rew.Mutations) == 0 {
		return nil, nil, nil
	}

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
			return nil, nil, nil
		}
	}

	overlayPath, cleanOverlay, err := runner.BuildOverlay(rew, pkg.Dir)
	if err != nil {
		return nil, nil, fmt.Errorf("build overlay: %w", err)
	}
	defer cleanOverlay()

	binaryPath, cleanBin, err := runner.CompileTestBinary(ctx, pkg.ImportPath, overlayPath)
	if err != nil {
		return nil, nil, fmt.Errorf("compile test binary: %w", err)
	}
	defer cleanBin()

	// Separate cover-instrumented binary built without overlay; used only
	// for per-test coverage collection. See CompileCoverageBinary for why.
	covBinaryPath, cleanCovBin, err := runner.CompileCoverageBinary(ctx, pkg.ImportPath)
	if err != nil {
		return nil, nil, fmt.Errorf("compile coverage binary: %w", err)
	}
	defer cleanCovBin()

	inventory, err := runner.RunBaseline(ctx, binaryPath, pkg.Dir, cfg.timeout, cfg.testsRegex)
	if err != nil {
		return nil, nil, fmt.Errorf("baseline failed: %w", err)
	}

	// Collect per-test coverage once; pays off across all mutants.
	covMap, err := runner.CollectPerTestCoverage(ctx, covBinaryPath, pkg.Dir, inventory, cfg.timeout, cfg.jobs)
	if err != nil {
		return nil, nil, fmt.Errorf("collect coverage: %w", err)
	}

	// Per-mutant covering tests are read out up front so the parallel loop
	// below does not touch covMap concurrently and so we can populate the
	// `relevant` set (used to narrow the reported inventory) before any
	// goroutine starts. Indexed slices preserve schema-ID order across the
	// parallel phase, keeping JSON output byte-identical to --jobs=1.
	perMut := make([][]string, len(mutations))
	relevant := make(map[string]bool)
	for i, mut := range mutations {
		tests := covMap[runner.FileLine{File: mut.File, Line: mut.Line}]
		perMut[i] = tests
		for _, t := range tests {
			relevant[t] = true
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
			status, killingTests, dur, err := runner.RunMutant(gctx, binaryPath, mut.ID, pkg.Dir, tests, cfg.timeout)
			if err != nil {
				return fmt.Errorf("run mutant %d: %w", mut.ID, err)
			}
			results[i] = mutation.Result{
				Mutation:     mut,
				Status:       status,
				KillingTests: killingTests,
				Duration:     dur,
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
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

	return results, inventory, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	const usage = "usage: kanly [--format=text|json] [--timeout=30s] [--diff [--diff-base=<ref>]] [--tests=<regex>] [--mutant=<id-list>] [--jobs=N] <pattern>[:<func-list>]...\n"

	fs := flag.NewFlagSet("kanly", flag.ContinueOnError)
	fs.SetOutput(stderr)
	formatFlag := fs.String("format", "text", "output format: text|json")
	timeoutFlag := fs.Duration("timeout", 30*time.Second, "per-mutant test timeout")
	diffFlag := fs.Bool("diff", false, "only mutate lines changed since --diff-base")
	diffBaseFlag := fs.String("diff-base", "HEAD", "git ref to diff against when --diff is set")
	testsFlag := fs.String("tests", "", "regex narrowing the test inventory used by baseline and per-test coverage")
	mutantFlag := fs.String("mutant", "", "comma-separated schema-assigned mutant IDs to re-run; others are skipped")
	jobsFlag := fs.Int("jobs", runtime.NumCPU(), "parallel worker processes for per-test coverage and the mutant loop (1 = sequential)")

	if err := fs.Parse(args); err != nil {
		fmt.Fprint(stderr, usage)
		return 2
	}

	if *jobsFlag < 1 {
		fmt.Fprintf(stderr, "--jobs: must be >= 1, got %d\n", *jobsFlag)
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
			return writeReport(stdout, stderr, *formatFlag, report.Build(nil, nil))
		}
		if len(specs) == 0 {
			for _, p := range d.Patterns(cwd) {
				specs = append(specs, selector.Spec{Pattern: p})
			}
			if len(specs) == 0 {
				return writeReport(stdout, stderr, *formatFlag, report.Build(nil, nil))
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

	ctx := context.Background()
	var allResults []mutation.Result
	testInventory := make(map[string][]string)

	for _, spec := range specs {
		pkgs, err := source.LoadAll("", spec.Pattern)
		if err != nil {
			fmt.Fprintf(stderr, "error loading packages for %q: %v\n", spec.Pattern, err)
			return 1
		}
		if len(pkgs) == 0 {
			fmt.Fprintf(stderr, "no packages matched %q\n", spec.Pattern)
			return 1
		}
		if len(spec.Funcs) > 0 && len(pkgs) != 1 {
			fmt.Fprintf(stderr, "selector %q with function filter must resolve to one package; got %d\n", spec.Pattern, len(pkgs))
			return 1
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
				filter:     composeFilter(diffPred, spec.Funcs),
				timeout:    *timeoutFlag,
				testsRegex: *testsFlag,
				mutantIDs:  mutantIDs,
				jobs:       *jobsFlag,
			}

			results, inventory, err := runPackage(ctx, pkg, ops, cfg, stderr)
			if err != nil {
				fmt.Fprintf(stderr, "error in %s: %v\n", pkg.ImportPath, err)
				return 1
			}
			if len(results) == 0 {
				fmt.Fprintf(stderr, "skip %s: no mutations\n", pkg.ImportPath)
				continue
			}
			allResults = append(allResults, results...)
			testInventory[pkg.ImportPath] = inventory
		}
	}

	rep := report.Build(allResults, testInventory)
	rep.Scope = describeScope(specs, *diffFlag, *diffBaseFlag, *testsFlag, *mutantFlag)
	return writeReport(stdout, stderr, *formatFlag, rep)
}

// composeFilter combines an optional diff (file, line) predicate with an
// optional function-name selector list. Returns nil when neither is active so
// the rewriter takes the fast no-filter path.
func composeFilter(diffPred func(file string, line int) bool, funcs []string) func(string, int, string) bool {
	if diffPred == nil && len(funcs) == 0 {
		return nil
	}
	return func(file string, line int, funcName string) bool {
		if diffPred != nil && !diffPred(file, line) {
			return false
		}
		if len(funcs) > 0 && !selector.AnyMatch(funcs, funcName) {
			return false
		}
		return true
	}
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

func writeReport(stdout, stderr io.Writer, format string, r report.Report) int {
	switch format {
	case "json":
		if err := report.WriteJSON(stdout, r); err != nil {
			fmt.Fprintf(stderr, "write JSON: %v\n", err)
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
