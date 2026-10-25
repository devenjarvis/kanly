package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// fileScopeFunc is the synthetic "function name" used for mutations that aren't
// inside any top-level FuncDecl (rare, but possible for future operators).
const fileScopeFunc = "<file-scope>"

type Summary struct {
	Total      int `json:"total"`
	Killed     int `json:"killed"`
	Survived   int `json:"survived"`
	Timeout    int `json:"timeout"`
	NotViable  int `json:"not_viable"`
	NotCovered int `json:"not_covered"`
	// Ignored counts mutants suppressed by //kanly:ignore; they are not in
	// Total. Set on the report's overall summary only.
	Ignored int     `json:"ignored,omitempty"`
	Score   float64 `json:"score"`
	// ScoreWithUncovered also counts not-covered mutants as undetected, so
	// untested code lowers it; see scoreWithUncovered.
	ScoreWithUncovered float64 `json:"score_with_uncovered"`
}

// SchemaVersion is the JSON report's schema_version. It changes when a field
// is removed or changes meaning; added fields keep it.
const SchemaVersion = 2

// scoreWithUncovered is the fraction of viable mutants the tests detected,
// covered or not. Unlike score it cannot be raised by leaving code untested:
// a package with one covered, killed mutant and a thousand uncovered ones
// scores 100% on score but 0.1% here.
func scoreWithUncovered(s Summary) float64 {
	denominator := s.Total - s.NotViable
	if denominator <= 0 {
		return 0
	}
	return float64(s.Killed+s.Timeout) / float64(denominator)
}

// score is the mutation score: the fraction of covered, viable mutants the
// tests detected. A timeout counts as detected, as in PIT and Stryker: the
// mutant stopped the test suite from passing (usually by hanging it), and
// counting it otherwise would make the score depend on how long Kanly waits.
func score(s Summary) float64 {
	denominator := s.Total - s.NotCovered - s.NotViable
	if denominator <= 0 {
		return 0
	}
	return float64(s.Killed+s.Timeout) / float64(denominator)
}

type PackageSummary struct {
	Package string `json:"package"`
	Summary
}

// PackageError records a package that was matched but not mutation-tested.
// Stage names the pipeline step that failed: "load", "rewrite", "compile",
// "baseline", "coverage" or "mutants". A failed baseline means the test binary
// fails outside any individual test (TestMain, init, a crash); failing tests
// are excluded instead (see ExcludedTest). The error says whether the
// unmodified code fails too.
type PackageError struct {
	Package string `json:"package"`
	Stage   string `json:"stage"`
	Error   string `json:"error"`
}

// ExcludedTest records a test that was left out of mutation testing because
// it fails without any mutant applied, so it cannot tell a mutant apart from
// the original code.
type ExcludedTest struct {
	Package string `json:"package"`
	Test    string `json:"test"`
	Reason  string `json:"reason"`
}

// StaleIgnore is a .kanlyignore entry that matches no mutant in its
// package any more: the mutated line changed or went away.
type StaleIgnore struct {
	Package     string `json:"package"`
	File        string `json:"file"`
	Fingerprint string `json:"fingerprint"`
	Reason      string `json:"reason"`
}

type Report struct {
	SchemaVersion int              `json:"schema_version"`
	Scope         string           `json:"scope,omitempty"`
	Summary       Summary          `json:"summary"`
	Packages      []PackageSummary `json:"packages"`
	Errors        []PackageError   `json:"errors,omitempty"`
	ExcludedTests []ExcludedTest   `json:"excluded_tests,omitempty"`
	StaleIgnores  []StaleIgnore    `json:"stale_ignores,omitempty"`
	// UnmatchedMutants lists --mutant fingerprints that matched no mutant
	// in scope: the mutated line changed, or the mutant was accepted.
	UnmatchedMutants []string `json:"unmatched_mutants,omitempty"`
	// FailFast is set when each mutant stopped at its first failing test,
	// the default unless --kill-matrix: killing_tests then names that test
	// only, and the test-level views (tests, zero_kill_tests,
	// redundant_test_groups) are left empty because partial kill sets would
	// make them wrong.
	FailFast bool `json:"fail_fast,omitempty"`
	// CrossPackage is set when the tests of the module's other packages
	// that import a mutated package ran against its mutants too
	// (--cross-package). Their names in killing_tests and covering_tests
	// are qualified by import path; the package's own tests' are not.
	CrossPackage        bool                         `json:"cross_package,omitempty"`
	Tests               []mutation.TestStats         `json:"tests"`
	ZeroKillTests       []string                     `json:"zero_kill_tests"`
	RedundantTestGroups [][]string                   `json:"redundant_test_groups"`
	SurvivorsByFunction []mutation.FunctionSurvivors `json:"survivors_by_function"`
	Mutants             []mutation.Result            `json:"mutants"`
}

// Build assembles a Report from per-mutant Results plus the per-package test
// inventory.
func Build(results []mutation.Result, testInventory map[string][]string) Report {
	var s Summary
	s.Total = len(results)
	for _, r := range results {
		switch r.Status {
		case mutation.StatusKilled:
			s.Killed++
		case mutation.StatusSurvived:
			s.Survived++
		case mutation.StatusTimeout:
			s.Timeout++
		case mutation.StatusNotViable:
			s.NotViable++
		case mutation.StatusNotCovered:
			s.NotCovered++
		}
	}
	s.Score = score(s)
	s.ScoreWithUncovered = scoreWithUncovered(s)

	// Build per-package summaries.
	pkgMap := make(map[string]*Summary)
	pkgOrder := make(map[string]struct{})
	for _, r := range results {
		pkg := r.Mutation.Package
		if _, ok := pkgMap[pkg]; !ok {
			pkgMap[pkg] = &Summary{}
			pkgOrder[pkg] = struct{}{}
		}
		ps := pkgMap[pkg]
		ps.Total++
		switch r.Status {
		case mutation.StatusKilled:
			ps.Killed++
		case mutation.StatusSurvived:
			ps.Survived++
		case mutation.StatusTimeout:
			ps.Timeout++
		case mutation.StatusNotViable:
			ps.NotViable++
		case mutation.StatusNotCovered:
			ps.NotCovered++
		}
	}

	pkgNames := make([]string, 0, len(pkgMap))
	for name := range pkgOrder {
		pkgNames = append(pkgNames, name)
	}
	sort.Strings(pkgNames)

	pkgSummaries := make([]PackageSummary, 0, len(pkgNames))
	for _, name := range pkgNames {
		ps := pkgMap[name]
		ps.Score = score(*ps)
		ps.ScoreWithUncovered = scoreWithUncovered(*ps)
		pkgSummaries = append(pkgSummaries, PackageSummary{Package: name, Summary: *ps})
	}

	tests, zeroKill, redundant := buildTestAggregations(results, testInventory)
	survivors := buildSurvivorsByFunction(results)

	return Report{
		SchemaVersion:       SchemaVersion,
		Summary:             s,
		Packages:            pkgSummaries,
		Tests:               tests,
		ZeroKillTests:       zeroKill,
		RedundantTestGroups: redundant,
		SurvivorsByFunction: survivors,
		Mutants:             results,
	}
}

// buildTestAggregations flips per-mutant kill data around the test axis and
// derives the zero-kill list and redundant-test groups.
func buildTestAggregations(results []mutation.Result, testInventory map[string][]string) ([]mutation.TestStats, []string, [][]string) {
	type key struct{ pkg, name string }
	stats := make(map[key]*mutation.TestStats)

	ensure := func(pkg, name string) *mutation.TestStats {
		k := key{pkg, name}
		if ts, ok := stats[k]; ok {
			return ts
		}
		ts := &mutation.TestStats{Package: pkg, Name: name}
		stats[k] = ts
		return ts
	}

	for pkg, names := range testInventory {
		for _, n := range names {
			ensure(pkg, n)
		}
	}

	for _, r := range results {
		for _, name := range r.KillingTests {
			ts := ensure(r.Mutation.Package, name)
			ts.KilledMutants = append(ts.KilledMutants, r.Mutation.ID)
		}
	}

	tests := make([]mutation.TestStats, 0, len(stats))
	for _, ts := range stats {
		sort.Ints(ts.KilledMutants)
		ts.KillCount = len(ts.KilledMutants)
		tests = append(tests, *ts)
	}
	sort.Slice(tests, func(i, j int) bool {
		if tests[i].KillCount != tests[j].KillCount {
			return tests[i].KillCount > tests[j].KillCount
		}
		if tests[i].Package != tests[j].Package {
			return tests[i].Package < tests[j].Package
		}
		return tests[i].Name < tests[j].Name
	})

	var zeroKill []string
	for _, ts := range tests {
		if ts.KillCount > 0 {
			continue
		}
		zeroKill = append(zeroKill, qualifyTestName(ts.Package, ts.Name))
	}
	sort.Strings(zeroKill)

	// Group tests sharing identical non-empty kill-sets. Mutation IDs are
	// unique per package only, so the package is part of the key.
	byHash := make(map[string][]string)
	for _, ts := range tests {
		if ts.KillCount == 0 {
			continue
		}
		ids := make([]string, len(ts.KilledMutants))
		for i, id := range ts.KilledMutants {
			ids[i] = strconv.Itoa(id)
		}
		h := ts.Package + "\x00" + strings.Join(ids, ",")
		byHash[h] = append(byHash[h], qualifyTestName(ts.Package, ts.Name))
	}
	var redundant [][]string
	for _, group := range byHash {
		if len(group) < 2 {
			continue
		}
		sort.Strings(group)
		redundant = append(redundant, group)
	}
	sort.Slice(redundant, func(i, j int) bool {
		return redundant[i][0] < redundant[j][0]
	})

	return tests, zeroKill, redundant
}

// buildSurvivorsByFunction groups every surviving mutation by its enclosing
// (package, function), producing a deterministically-sorted slice for navigation.
func buildSurvivorsByFunction(results []mutation.Result) []mutation.FunctionSurvivors {
	type key struct{ pkg, fn string }
	groups := make(map[key]*mutation.FunctionSurvivors)
	for _, r := range results {
		if r.Status != mutation.StatusSurvived {
			continue
		}
		fn := r.Mutation.Function
		if fn == "" {
			fn = fileScopeFunc
		}
		k := key{r.Mutation.Package, fn}
		g, ok := groups[k]
		if !ok {
			g = &mutation.FunctionSurvivors{Package: r.Mutation.Package, Function: fn}
			groups[k] = g
		}
		g.Mutations = append(g.Mutations, r.Mutation)
	}
	out := make([]mutation.FunctionSurvivors, 0, len(groups))
	for _, g := range groups {
		sort.Slice(g.Mutations, func(i, j int) bool {
			if g.Mutations[i].Line != g.Mutations[j].Line {
				return g.Mutations[i].Line < g.Mutations[j].Line
			}
			return g.Mutations[i].Column < g.Mutations[j].Column
		})
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Package != out[j].Package {
			return out[i].Package < out[j].Package
		}
		return out[i].Function < out[j].Function
	})
	return out
}

// qualifyTestName prefixes a test name with its package. Names of another
// package's tests (--cross-package) are qualified already: a Go identifier
// cannot contain a dot.
func qualifyTestName(pkg, name string) string {
	if pkg == "" || strings.Contains(name, ".") {
		return name
	}
	return pkg + "." + name
}

// LLMSource carries the extra context the LLM-ready Markdown renderer needs
// beyond the bare Report: per-package function source ranges (so the renderer
// can slice the enclosing function out of each file) and an injectable
// ReadFile so tests can supply fake source content. When ReadFile is nil the
// renderer falls back to os.ReadFile.
type LLMSource struct {
	FuncRanges map[string]map[string]mutation.FuncRange
	ReadFile   func(string) ([]byte, error)
	// Limit caps the mutants listed, live ones first; the functions left
	// out are named. 0 lists every mutant.
	Limit int
}

// llmWarnSize triggers a stderr-style warning when the rendered artifact
// exceeds this many bytes; an LLM prompt much larger than this is usually a
// sign the user should add a --diff / --tests / pkg:func scope.
const llmWarnSize = 200 * 1024

// WriteLLM renders the report as a Markdown artifact tailored for feeding to
// an LLM that will write new tests for survivors and consolidate redundant
// ones. The layout puts the highest-leverage data (surviving mutants with
// source context + the tests that almost killed them) above the
// consolidation and inventory sections.
//
// The structure follows the prescription validated by Wang, Xu, Briand & Liu
// (2025), "Mutation-Guided Unit Test Generation with a Large Language Model"
// (arXiv:2506.02954): the task is framed explicitly, live (covered-but-
// survived) mutants are surfaced separately from uncovered mutants because
// they need different test strategies, and the iterate-after-edit loop using
// `--mutant=<ids>` mirrors MUTGEN's iterative generation mechanism.
func WriteLLM(w io.Writer, r Report, src LLMSource) error {
	readFile := src.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}

	var b strings.Builder
	b.WriteString("# Kanly mutation report\n\n")
	if r.Scope != "" {
		fmt.Fprintf(&b, "**Scope:** `%s`\n\n", r.Scope)
	}

	writeLLMPreamble(&b)
	writeLLMSummary(&b, r.Summary)
	survivors, uncovered := splitUnkilled(r.Mutants)
	budget := &llmBudget{left: src.Limit, limit: src.Limit > 0}
	survivors, omittedLive := budget.take(survivors)
	uncovered, omittedUncovered := budget.take(uncovered)
	writeLLMLiveSection(&b, r, survivors, src.FuncRanges, readFile)
	writeLLMOmitted(&b, "live", omittedLive, src.Limit)
	writeLLMUncoveredSection(&b, r, uncovered, src.FuncRanges, readFile)
	writeLLMOmitted(&b, "uncovered", omittedUncovered, src.Limit)
	if r.FailFast {
		b.WriteString("_Test-level analysis skipped: each mutant stopped at its first failing test, so kill sets are incomplete. Run with `--kill-matrix` for it._\n\n")
	} else {
		writeLLMRedundant(&b, r.RedundantTestGroups)
		writeLLMZeroKill(&b, r)
		writeLLMTestInventory(&b, r.Tests)
	}
	writeLLMErrors(&b, r.Errors)
	writeLLMExcluded(&b, r.ExcludedTests)
	writeLLMStale(&b, r.StaleIgnores)
	writeLLMUnmatched(&b, r.UnmatchedMutants)
	writeLLMNextIteration(&b, survivors, uncovered)

	out := b.String()
	if _, err := io.WriteString(w, out); err != nil {
		return err
	}
	if len(out) > llmWarnSize {
		fmt.Fprintf(os.Stderr, "kanly: llm output is %d bytes; consider scoping with --diff, --tests, or pkg:func\n", len(out))
	}
	return nil
}

// writeLLMPreamble renders the task framing so an LLM consuming the artifact
// has explicit instructions without the user having to wrap it in their own
// system prompt. Mutation score (not line coverage) is named as the goal,
// matching the headline finding of arXiv:2506.02954. A test is only one of
// three outcomes: optimizing for the score alone pushes a model toward
// contorted tests for equivalent mutants and toward change-detector tests
// that pin implementation details.
func writeLLMPreamble(b *strings.Builder) {
	b.WriteString("## Task\n\n")
	b.WriteString("Raise the **mutation score**: each mutant below is a small change to the code that no test noticed. Line coverage is not the target. Decide each mutant one of three ways:\n\n")
	b.WriteString("1. **Test it.** For a live mutant (tests run the line but none failed), sharpen an assertion so the original and the mutant diverge. For an uncovered mutant, add a test that reaches the line.\n")
	b.WriteString("2. **Simplify the code.** A mutant nothing can observe may reveal dead or redundant code, such as a field that is never read or a check repeated later. Delete it and the mutant goes with it.\n")
	b.WriteString("3. **Accept it.** If the mutant is equivalent (no input tells it apart from the original) or only a contrived test could kill it, append `<fp> <reason>` to `" + ignoreFileName + "` in the package's directory. Kanly stops reporting it until its line changes.\n\n")
	b.WriteString("Write tests a maintainer would keep: assert on behaviour callers can observe through the package's API. Match errors with `errors.Is`/`errors.As` or a key substring, not the full message, and don't assert on unexported fields, log text or call order just to kill a mutant. A test that breaks on a harmless refactor costs more than a surviving mutant; accept the mutant instead.\n\n")
}

// ignoreFileName is schema.IgnoreFileName, which report cannot import.
const ignoreFileName = ".kanlyignore"

func writeLLMSummary(b *strings.Builder, s Summary) {
	b.WriteString("## Summary\n\n")
	b.WriteString("| Total | Killed | Survived | Timeout | NotCovered | NotViable | Score | Score incl. not covered |\n")
	b.WriteString("|-------|--------|----------|---------|------------|-----------|-------|-------------------------|\n")
	fmt.Fprintf(b, "| %d | %d | %d | %d | %d | %d | %.1f%% | %.1f%% |\n\n",
		s.Total, s.Killed, s.Survived, s.Timeout, s.NotCovered, s.NotViable, s.Score*100, s.ScoreWithUncovered*100)
}

// llmBudget counts the mutants the brief may still list.
type llmBudget struct {
	left   int
	listed int
	limit  bool // false lists everything
}

// take keeps whole groups, in order, while their mutants fit, and returns
// the rest. The brief's first group is always kept, so a limit smaller than
// one function's mutants still lists that function.
func (bud *llmBudget) take(groups []mutation.FunctionSurvivors) (kept, omitted []mutation.FunctionSurvivors) {
	if !bud.limit {
		return groups, nil
	}
	for i, g := range groups {
		if len(g.Mutations) > bud.left && bud.listed > 0 {
			return kept, groups[i:]
		}
		bud.left -= len(g.Mutations)
		bud.listed += len(g.Mutations)
		kept = append(kept, g)
	}
	return kept, nil
}

// splitUnkilled groups survived and not-covered mutants by (package,
// function), returning them as two parallel slices. The split mirrors the
// MUTGEN distinction (arXiv:2506.02954) — covered-but-survived needs an
// assertion-strength fix; uncovered needs a new test to reach the line.
func splitUnkilled(results []mutation.Result) (survived, notCovered []mutation.FunctionSurvivors) {
	return groupByFunction(results, mutation.StatusSurvived),
		groupByFunction(results, mutation.StatusNotCovered)
}

// groupByFunction buckets results matching `want` by (package, function) and
// returns the groups in package-then-function order, each with mutations
// sorted by line and column.
func groupByFunction(results []mutation.Result, want mutation.Status) []mutation.FunctionSurvivors {
	type key struct{ pkg, fn string }
	groups := make(map[key]*mutation.FunctionSurvivors)
	for _, r := range results {
		if r.Status != want {
			continue
		}
		fn := r.Mutation.Function
		if fn == "" {
			fn = fileScopeFunc
		}
		k := key{r.Mutation.Package, fn}
		g, ok := groups[k]
		if !ok {
			g = &mutation.FunctionSurvivors{Package: r.Mutation.Package, Function: fn}
			groups[k] = g
		}
		g.Mutations = append(g.Mutations, r.Mutation)
	}
	out := make([]mutation.FunctionSurvivors, 0, len(groups))
	for _, g := range groups {
		sort.Slice(g.Mutations, func(i, j int) bool {
			if g.Mutations[i].Line != g.Mutations[j].Line {
				return g.Mutations[i].Line < g.Mutations[j].Line
			}
			return g.Mutations[i].Column < g.Mutations[j].Column
		})
		out = append(out, *g)
	}
	// Functions with the most mutants come first: one sharper test there
	// settles the most, and a limited brief keeps them.
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Mutations) != len(out[j].Mutations) {
			return len(out[i].Mutations) > len(out[j].Mutations)
		}
		if out[i].Package != out[j].Package {
			return out[i].Package < out[j].Package
		}
		return out[i].Function < out[j].Function
	})
	return out
}

// maxOmittedNamed caps the functions named in a note on omitted mutants.
const maxOmittedNamed = 10

// writeLLMOmitted names the functions whose mutants of one kind ("live" or
// "uncovered") the limit left out of the brief.
func writeLLMOmitted(b *strings.Builder, kind string, groups []mutation.FunctionSurvivors, limit int) {
	if len(groups) == 0 {
		return
	}
	n := 0
	var names []string
	for _, g := range groups {
		n += len(g.Mutations)
		if len(names) < maxOmittedNamed {
			names = append(names, fmt.Sprintf("`%s:%s` (%d)", g.Package, g.Function, len(g.Mutations)))
		}
	}
	more := ""
	if len(groups) > len(names) {
		more = fmt.Sprintf(" and %d more", len(groups)-len(names))
	}
	fmt.Fprintf(b, "_%d more %s mutant(s) are not listed (`--llm-limit=%d`): %s%s. Settle the ones above first, then re-run, or run `kanly --format=llm <pkg>:<Func>` for one of these._\n\n", n, kind, limit, strings.Join(names, ", "), more)
}

// operatorHints maps each registered operator ID to a one-line test-strategy
// hint. Emitted on first occurrence within a section so the LLM gets a nudge
// toward the inputs/assertions that distinguish original from mutant for that
// operator class.
var operatorHints = map[string]string{
	"int_arith":           "Use inputs where +/-/*/% differ; avoid zero/identity values that mask the operator.",
	"int_cmp_boundary":    "Test exactly at the boundary (n and n±1) so `<` vs `<=` flips the result.",
	"int_cmp_negate":      "Assert on both truthy and falsy inputs so a negated comparison can't pass both.",
	"int_bitwise":         "Choose bit patterns where `&` and `|` differ (e.g. 0b10 and 0b01) and assert the exact value.",
	"int_literal":         "Assert the exact numeric result so swapping the literal to 0/1/±1 fails. A size or capacity that only affects performance is usually not worth a test.",
	"bool_logic":          "Pick inputs where `&&` and `||` diverge (one true, one false) and assert the boolean result.",
	"bool_not":            "Assert the boolean direction explicitly — removing `!` should flip the test.",
	"bool_literal":        "Assert the exact boolean value (not just truthiness) so `true↔false` is caught.",
	"err_return_nil":      "Drive the function down the error branch and assert `err != nil`; check its kind with `errors.Is`/`errors.As`, not its message.",
	"return_zero":         "Assert the returned value, not just `err == nil`; a zero return must fail the test.",
	"call_delete":         "Assert the call's observable effect (state, output, error). If removing the call changes nothing observable, the call may be unnecessary.",
	"string_literal":      "Assert strings callers depend on (output formats, keys, user-facing text). For an error message, prefer `errors.Is` or a key substring over the full text: emptying a `fmt.Errorf` format also drops its `%w` wrap.",
	"slice_index":         "Use distinct values at adjacent indices and assert the exact element, not just presence.",
	"slice_range":         "Test inputs where shifting the slice bounds by one would change the result.",
	"inc_dec":             "Assert the post-loop counter / iteration count so `++↔--` diverges visibly.",
	"int_compound_assign": "Pick operands where the compound op's direction matters and assert the exact accumulator value.",
	"struct_field_zero":   "Assert the field's effect through the API, not just that the struct is non-nil. If nothing reads the field, delete it.",
	"float_arith":         "Use inputs where `+ - * /` give different results (avoid 0 and 1) and assert the value within a tight tolerance.",
	"float_cmp_boundary":  "Test exactly at the threshold so `<` vs `<=` flips the result.",
	"float_cmp_negate":    "Assert on inputs on both sides of the comparison so a negated one can't pass both.",
	"chan_op":             "Assert the value received, or that the message arrives (a `select` with a timeout), so a removed send or receive fails rather than hangs.",
	"switch_case":         "Test an input for each case and assert its distinct result, so dropping a case or swapping two bodies changes the outcome.",
}

// writeLLMUnkilledGroups renders one offense section (live or uncovered),
// emitting the per-function snippet, the per-mutant line, and an inline
// operator-hint legend that fires once per operator on first occurrence.
func writeLLMUnkilledGroups(b *strings.Builder, groups []mutation.FunctionSurvivors, byID map[mutantKey]mutation.Result, funcRanges map[string]map[string]mutation.FuncRange, readFile func(string) ([]byte, error)) {
	seenHints := make(map[string]bool)
	for _, g := range groups {
		fmt.Fprintf(b, "### %s — %s  (%d)\n\n", g.Package, g.Function, len(g.Mutations))

		snippet := false
		if pkgRanges, ok := funcRanges[g.Package]; ok {
			if fr, ok := pkgRanges[g.Function]; ok {
				snippet = writeLLMSnippet(b, fr, readFile)
			}
		}

		for _, m := range g.Mutations {
			res := byID[mutantKey{m.Package, m.ID}]
			// The snippet header names the file; repeating it per mutant
			// is the costliest part of a large report.
			loc := fmt.Sprintf("L%d:%d", m.Line, m.Column)
			if !snippet {
				loc = fmt.Sprintf("%s:%d:%d", filepath.ToSlash(relPath(m.File)), m.Line, m.Column)
			}
			fmt.Fprintf(b, "- **#%d** %s `%s` `%s` → `%s`", m.ID, loc, m.OperatorName, m.Original, m.Mutant)
			if m.Fingerprint != "" {
				fmt.Fprintf(b, " · fp `%s`", m.Fingerprint)
			}
			writeLLMCoveringTests(b, res)
			if res.Note != "" {
				fmt.Fprintf(b, " · %s", res.Note)
			}
			b.WriteString("\n")
			if hint, ok := operatorHints[m.OperatorName]; ok && !seenHints[m.OperatorName] {
				fmt.Fprintf(b, "  - _Hint (`%s`): %s_\n", m.OperatorName, hint)
				seenHints[m.OperatorName] = true
			}
		}
		b.WriteString("\n")
	}
}

func writeLLMLiveSection(b *strings.Builder, r Report, groups []mutation.FunctionSurvivors, funcRanges map[string]map[string]mutation.FuncRange, readFile func(string) ([]byte, error)) {
	b.WriteString("## Live mutants (covered but not killed)\n\n")
	b.WriteString("_Tests run these lines, but none failed when the mutant was applied. Sharpen an assertion, simplify the code, or accept the mutant._\n\n")
	if len(groups) == 0 {
		b.WriteString("_None — every covered mutant was killed._\n\n")
		return
	}
	byID := resultsByKey(r.Mutants)
	writeLLMUnkilledGroups(b, groups, byID, funcRanges, readFile)
}

func writeLLMUncoveredSection(b *strings.Builder, r Report, groups []mutation.FunctionSurvivors, funcRanges map[string]map[string]mutation.FuncRange, readFile func(string) ([]byte, error)) {
	b.WriteString("## Uncovered mutants (no test reaches this line)\n\n")
	if r.CrossPackage {
		b.WriteString("_No test in the package, or in the module's packages that import it, runs these lines. Add a test that drives the path; almost any meaningful assertion on the result will catch the mutation._\n\n")
	} else {
		b.WriteString("_No test in the package runs these lines. Add a test that drives the path; almost any meaningful assertion on the result will catch the mutation. Code exercised only by another package's tests shows up here too: re-run with `--cross-package` to count those tests before writing new ones._\n\n")
	}
	if len(groups) == 0 {
		b.WriteString("_None — every mutation site is reached by at least one test._\n\n")
		return
	}
	byID := resultsByKey(r.Mutants)
	writeLLMUnkilledGroups(b, groups, byID, funcRanges, readFile)
}

// mutantKey identifies a mutant in a report: IDs are unique per package only.
type mutantKey struct {
	pkg string
	id  int
}

func resultsByKey(results []mutation.Result) map[mutantKey]mutation.Result {
	byKey := make(map[mutantKey]mutation.Result, len(results))
	for _, res := range results {
		byKey[mutantKey{res.Mutation.Package, res.Mutation.ID}] = res
	}
	return byKey
}

// writeLLMNextIteration renders the closing instruction pointing at the
// `--mutant=<ids>` re-verification loop. Skipped when there's nothing to
// iterate on, so a clean-suite artifact ends on the inventory.
func writeLLMNextIteration(b *strings.Builder, survived, notCovered []mutation.FunctionSurvivors) {
	// Fingerprints survive edits elsewhere in the package, which renumber
	// its mutants; an ID is used only for a mutant without one.
	refsByPkg := make(map[string][]string)
	for _, groups := range [][]mutation.FunctionSurvivors{survived, notCovered} {
		for _, g := range groups {
			for _, m := range g.Mutations {
				ref := m.Fingerprint
				if ref == "" {
					ref = strconv.Itoa(m.ID)
				}
				refsByPkg[m.Package] = append(refsByPkg[m.Package], ref)
			}
		}
	}
	if len(refsByPkg) == 0 {
		return
	}
	pkgs := make([]string, 0, len(refsByPkg))
	for p := range refsByPkg {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)

	b.WriteString("## Next iteration\n\n")
	b.WriteString("After editing tests or code, re-run only these mutants (the iterative loop validated in arXiv:2506.02954):\n\n")
	b.WriteString("```\n")
	for _, pkg := range pkgs {
		refs := refsByPkg[pkg]
		sort.Strings(refs)
		fmt.Fprintf(b, "kanly --mutant=%s %s\n", strings.Join(refs, ","), pkg)
	}
	b.WriteString("```\n\n")
	b.WriteString("Mutants are named by fingerprint (`fp`), which survives edits elsewhere in the package. Editing a mutant's own line gives it a new fingerprint, and the re-run reports the old one as not found: re-run the package or `<pkg>:<Func>` to judge that line again.\n\n")
	b.WriteString("To try one test against one mutant while you write it, run `kanly test <pkg> <fp> -run <TestName>`: it runs `go test` with the mutant applied, so the test should fail. Add `--in=<pkg>` for a test in another package. `kanly show <pkg> <fp>` prints the mutated line.\n\n")
}

// writeLLMUnmatched lists --mutant fingerprints that matched no mutant.
func writeLLMUnmatched(b *strings.Builder, fps []string) {
	if len(fps) == 0 {
		return
	}
	b.WriteString("## Mutants not found\n\n")
	fmt.Fprintf(b, "No mutant in scope has these fingerprints: %s. The mutated line changed (which also changes the fingerprint) or the mutant is accepted in `%s`. Re-run the package or `<pkg>:<Func>` to see the line's current mutants.\n\n", strings.Join(backtickAll(fps), ", "), ignoreFileName)
}

// writeLLMSnippet writes the function's numbered source and reports whether
// it did.
func writeLLMSnippet(b *strings.Builder, fr mutation.FuncRange, readFile func(string) ([]byte, error)) bool {
	file := filepath.ToSlash(relPath(fr.File))
	data, err := readFile(fr.File)
	if err != nil {
		fmt.Fprintf(b, "_Source unavailable for %s: %v_\n\n", file, err)
		return false
	}
	lines := strings.Split(string(data), "\n")
	if fr.StartLine < 1 || fr.StartLine > len(lines) {
		return false
	}
	end := fr.EndLine
	if end > len(lines) {
		end = len(lines)
	}
	fmt.Fprintf(b, "**Source: %s:%d-%d**\n\n", file, fr.StartLine, end)
	b.WriteString("```go\n")
	width := len(strconv.Itoa(end))
	for i := fr.StartLine; i <= end; i++ {
		fmt.Fprintf(b, "%*d  %s\n", width, i, lines[i-1])
	}
	b.WriteString("```\n\n")
	return true
}

// maxLLMCoveringTests caps the covering tests listed per mutant; the JSON
// report has them all.
const maxLLMCoveringTests = 5

// writeLLMCoveringTests appends the tests that ran a live mutant without
// failing. The package's own tests are unqualified, since the section
// heading names the package; another package's are shortened to the last
// element of its import path.
func writeLLMCoveringTests(b *strings.Builder, res mutation.Result) {
	if res.Status == mutation.StatusNotCovered || len(res.CoveringTests) == 0 {
		return
	}
	killers := make(map[string]bool, len(res.KillingTests))
	for _, t := range res.KillingTests {
		killers[t] = true
	}
	var names []string
	for _, t := range res.CoveringTests {
		if !killers[t] {
			names = append(names, shortTestName(t))
		}
	}
	if len(names) == 0 {
		return
	}
	sort.Strings(names)
	more := ""
	if len(names) > maxLLMCoveringTests {
		more = fmt.Sprintf(" +%d more", len(names)-maxLLMCoveringTests)
		names = names[:maxLLMCoveringTests]
	}
	fmt.Fprintf(b, " · ran by %s%s", strings.Join(backtickAll(names), ", "), more)
}

// shortTestName shortens "example.com/m/app.TestX" to "app.TestX".
func shortTestName(name string) string {
	return name[strings.LastIndex(name, "/")+1:]
}

func backtickAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = "`" + s + "`"
	}
	return out
}

func writeLLMRedundant(b *strings.Builder, groups [][]string) {
	b.WriteString("## Tests with identical kill sets\n\n")
	if len(groups) == 0 {
		b.WriteString("_None — no two tests share an identical kill set._\n\n")
		return
	}
	b.WriteString("Each group's tests kill exactly the same mutants. Tests that check different behaviour along the same code path often do, so this alone is no reason to delete one; merge tests only if they also assert the same thing.\n\n")
	for _, g := range groups {
		fmt.Fprintf(b, "- %s\n", strings.Join(backtickAll(g), ", "))
	}
	b.WriteString("\n")
}

// writeLLMZeroKill lists the tests that killed nothing, flagging the ones
// that ran a live mutant: only those point at a weak assertion.
func writeLLMZeroKill(b *strings.Builder, r Report) {
	b.WriteString("## Tests that killed no mutants\n\n")
	if len(r.ZeroKillTests) == 0 {
		b.WriteString("_None — every test killed at least one mutant._\n\n")
		return
	}
	b.WriteString("**Do not delete these tests on this basis.** Most exercise code with no mutable operators (constructors, wiring, struct tags) or check behaviour Kanly's operators don't change. A test that ran a live mutant above may have a weak assertion; those are marked.\n\n")
	ranLive := make(map[string]bool)
	for _, res := range r.Mutants {
		if res.Status != mutation.StatusSurvived {
			continue
		}
		for _, t := range res.CoveringTests {
			ranLive[qualifyTestName(res.Mutation.Package, t)] = true
		}
	}
	for _, t := range r.ZeroKillTests {
		if ranLive[t] {
			fmt.Fprintf(b, "- `%s` — ran a live mutant\n", t)
		} else {
			fmt.Fprintf(b, "- `%s`\n", t)
		}
	}
	b.WriteString("\n")
}

// writeLLMStale lists .kanlyignore entries that no longer match a mutant.
func writeLLMStale(b *strings.Builder, stale []StaleIgnore) {
	if len(stale) == 0 {
		return
	}
	b.WriteString("## Stale " + ignoreFileName + " entries\n\n")
	b.WriteString("These entries match no mutant: the line changed or was removed. Delete them, and judge any mutant on that line again.\n\n")
	for _, s := range stale {
		fmt.Fprintf(b, "- `%s`: `%s` %s\n", filepath.ToSlash(relPath(s.File)), s.Fingerprint, s.Reason)
	}
	b.WriteString("\n")
}

// errorExcerptLines caps how much of a package's error the text and llm
// renderers print; the JSON report carries the full message.
const errorExcerptLines = 15

// errorExcerpt returns the first maxLines lines of msg, noting how many were
// dropped.
func errorExcerpt(msg string, maxLines int) string {
	lines := strings.Split(strings.TrimRight(msg, "\n"), "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:maxLines], "\n") + fmt.Sprintf("\n… (%d more lines; see --format=json)", len(lines)-maxLines)
}

func writeLLMErrors(b *strings.Builder, errs []PackageError) {
	if len(errs) == 0 {
		return
	}
	b.WriteString("## Packages not tested\n\n")
	b.WriteString("These packages matched but produced no mutation results, so the scores above do not cover them. A `baseline` failure means the test binary fails outside any single test (for example in `TestMain`); the error says whether the unmodified code fails too.\n\n")
	for _, e := range errs {
		fmt.Fprintf(b, "### %s (%s)\n\n```\n%s\n```\n\n", e.Package, e.Stage, errorExcerpt(e.Error, errorExcerptLines))
	}
}

func writeLLMExcluded(b *strings.Builder, tests []ExcludedTest) {
	if len(tests) == 0 {
		return
	}
	b.WriteString("## Tests excluded\n\n")
	b.WriteString("These tests fail without any mutant applied, so they were not run against mutants and their kills are not counted. Fix them to bring them back into the analysis.\n\n")
	for _, t := range tests {
		fmt.Fprintf(b, "- `%s`: %s\n", qualifyTestName(t.Package, t.Test), t.Reason)
	}
	b.WriteString("\n")
}

// writeLLMTestInventory lists each package's tests with their kill counts,
// most kills first. The killed IDs are in the JSON report.
func writeLLMTestInventory(b *strings.Builder, tests []mutation.TestStats) {
	b.WriteString("## Test inventory\n\n")
	if len(tests) == 0 {
		b.WriteString("_No tests in scope._\n\n")
		return
	}
	b.WriteString("Mutants killed per test:\n\n")
	byPkg := make(map[string][]string)
	var pkgs []string
	for _, ts := range tests {
		if _, ok := byPkg[ts.Package]; !ok {
			pkgs = append(pkgs, ts.Package)
		}
		byPkg[ts.Package] = append(byPkg[ts.Package], fmt.Sprintf("`%s` %d", ts.Name, ts.KillCount))
	}
	sort.Strings(pkgs)
	for _, pkg := range pkgs {
		fmt.Fprintf(b, "- `%s`: %s\n", pkg, strings.Join(byPkg[pkg], ", "))
	}
	b.WriteString("\n")
}

func WriteJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func WriteText(w io.Writer, r Report) error {
	if r.Scope != "" {
		if _, err := fmt.Fprintf(w, "Scope: %s\n\n", r.Scope); err != nil {
			return err
		}
	}
	// Unkilled mutants first — they are the actionable signal.
	for _, sec := range []struct {
		title  string
		status mutation.Status
	}{
		{"Survived (tests ran the mutated code but none failed)", mutation.StatusSurvived},
		{"Not covered (no test runs the mutated code)", mutation.StatusNotCovered},
	} {
		muts := sortedByLocation(r.Mutants, sec.status)
		if len(muts) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(w, "%s:\n", sec.title); err != nil {
			return err
		}
		for _, m := range muts {
			id := strconv.Itoa(m.Mutation.ID)
			if m.Mutation.Fingerprint != "" {
				id += ", fp " + m.Mutation.Fingerprint
			}
			line := fmt.Sprintf("  %s:%d:%d [%s] %s→%s (id %s)",
				relPath(m.Mutation.File), m.Mutation.Line, m.Mutation.Column,
				m.Mutation.OperatorName, m.Mutation.Original, m.Mutation.Mutant, id)
			if m.Note != "" {
				line += " — " + m.Note
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	for _, ps := range r.Packages {
		if _, err := fmt.Fprintf(w, "Package: %s | %s\n", ps.Package, summaryLine(ps.Summary)); err != nil {
			return err
		}
	}
	total := "\n" + summaryLine(r.Summary)
	if r.Summary.Ignored > 0 {
		total += fmt.Sprintf(" | Ignored: %d", r.Summary.Ignored)
	}
	if _, err := fmt.Fprintln(w, total); err != nil {
		return err
	}

	if len(r.Errors) > 0 {
		if _, err := fmt.Fprintf(w, "\nPackages not tested (%d):\n", len(r.Errors)); err != nil {
			return err
		}
		for _, e := range r.Errors {
			excerpt := strings.ReplaceAll(errorExcerpt(e.Error, errorExcerptLines), "\n", "\n    ")
			if _, err := fmt.Fprintf(w, "  %s [%s]: %s\n", e.Package, e.Stage, excerpt); err != nil {
				return err
			}
		}
	}

	if len(r.ExcludedTests) > 0 {
		if _, err := fmt.Fprintf(w, "\nTests excluded (%d):\n", len(r.ExcludedTests)); err != nil {
			return err
		}
		for _, t := range r.ExcludedTests {
			if _, err := fmt.Fprintf(w, "  %s: %s\n", qualifyTestName(t.Package, t.Test), t.Reason); err != nil {
				return err
			}
		}
	}

	if len(r.StaleIgnores) > 0 {
		if _, err := fmt.Fprintf(w, "\nStale %s entries, matching no mutant (%d):\n", ignoreFileName, len(r.StaleIgnores)); err != nil {
			return err
		}
		for _, s := range r.StaleIgnores {
			if _, err := fmt.Fprintf(w, "  %s: %s %s\n", relPath(s.File), s.Fingerprint, s.Reason); err != nil {
				return err
			}
		}
	}

	if len(r.UnmatchedMutants) > 0 {
		if _, err := fmt.Fprintf(w, "\n--mutant fingerprints matching no mutant (line changed or accepted): %s\n", strings.Join(r.UnmatchedMutants, ", ")); err != nil {
			return err
		}
	}

	if r.FailFast {
		if _, err := fmt.Fprintln(w, "\nTest-level analysis skipped: each mutant stopped at its first failing test. Run with --kill-matrix for it."); err != nil {
			return err
		}
	}

	// Top tests by kill count — up to 5 entries with at least one kill.
	var topKillers []mutation.TestStats
	for _, ts := range r.Tests {
		if ts.KillCount == 0 {
			continue
		}
		topKillers = append(topKillers, ts)
		if len(topKillers) == 5 {
			break
		}
	}
	if len(topKillers) > 0 {
		if _, err := fmt.Fprintln(w, "\nTop tests by kill count:"); err != nil {
			return err
		}
		for _, ts := range topKillers {
			if _, err := fmt.Fprintf(w, "  %s (%d)\n", qualifyTestName(ts.Package, ts.Name), ts.KillCount); err != nil {
				return err
			}
		}
	}

	if len(r.ZeroKillTests) > 0 {
		if _, err := fmt.Fprintln(w, "\nTests that killed nothing (by itself no reason to delete a test):"); err != nil {
			return err
		}
		for _, n := range r.ZeroKillTests {
			if _, err := fmt.Fprintf(w, "  %s\n", n); err != nil {
				return err
			}
		}
	}

	if len(r.RedundantTestGroups) > 0 {
		if _, err := fmt.Fprintln(w, "\nRedundant test groups (identical kill sets):"); err != nil {
			return err
		}
		for _, group := range r.RedundantTestGroups {
			if _, err := fmt.Fprintf(w, "  %s\n", strings.Join(group, ", ")); err != nil {
				return err
			}
		}
	}

	return nil
}

// summaryLine renders a Summary's counts and both scores on one line.
func summaryLine(s Summary) string {
	return fmt.Sprintf("Total: %d | Killed: %d | Survived: %d | Timeout: %d | Not covered: %d | Score: %.1f%% (%.1f%% incl. not covered)",
		s.Total, s.Killed, s.Survived, s.Timeout, s.NotCovered, s.Score*100, s.ScoreWithUncovered*100)
}

// sortedByLocation returns the results with any of the given statuses,
// ordered by file, line, column and ID.
func sortedByLocation(results []mutation.Result, statuses ...mutation.Status) []mutation.Result {
	var out []mutation.Result
	for _, r := range results {
		for _, st := range statuses {
			if r.Status == st {
				out = append(out, r)
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Mutation, out[j].Mutation
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return a.ID < b.ID
	})
	return out
}

// relPath returns an absolute path relative to the working directory when
// it lies beneath it, and the path unchanged otherwise.
func relPath(p string) string {
	if !filepath.IsAbs(p) {
		return p
	}
	cwd, err := os.Getwd()
	if err != nil {
		return p
	}
	if rel, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}
