package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// fileScopeFunc is the synthetic "function name" used for mutations that aren't
// inside any top-level FuncDecl (rare, but possible for future operators).
const fileScopeFunc = "<file-scope>"

type Summary struct {
	Total      int     `json:"total"`
	Killed     int     `json:"killed"`
	Survived   int     `json:"survived"`
	Timeout    int     `json:"timeout"`
	NotViable  int     `json:"not_viable"`
	NotCovered int     `json:"not_covered"`
	Score      float64 `json:"score"`
}

type PackageSummary struct {
	Package string `json:"package"`
	Summary
}

type Report struct {
	Scope                   string                       `json:"scope,omitempty"`
	Summary                 Summary                      `json:"summary"`
	Packages                []PackageSummary             `json:"packages"`
	Tests                   []mutation.TestStats         `json:"tests"`
	ZeroKillTests           []string                     `json:"zero_kill_tests"`
	IncidentalCoverageTests []string                     `json:"incidental_coverage_tests,omitempty"`
	RedundantTestGroups     [][]string                   `json:"redundant_test_groups"`
	SurvivorsByFunction     []mutation.FunctionSurvivors `json:"survivors_by_function"`
	Mutants                 []mutation.Result            `json:"mutants"`
}

// Build assembles a Report from per-mutant Results plus the per-package test
// inventory and the per-package incidental-coverage list (tests that the
// caller filtered out of the per-mutant runs because their statement coverage
// of a scoped line looked like setup traffic rather than an assertion). Pass
// nil for incidental on full / non-scoped runs to keep the report shape
// byte-identical to prior behavior — the field is `omitempty` and the LLM
// renderer's incidental section is silent when empty.
func Build(results []mutation.Result, testInventory map[string][]string, incidental map[string][]string) Report {
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
	denominator := s.Total - s.NotCovered - s.NotViable
	if denominator > 0 {
		s.Score = float64(s.Killed) / float64(denominator)
	}

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
		denom := ps.Total - ps.NotCovered - ps.NotViable
		if denom > 0 {
			ps.Score = float64(ps.Killed) / float64(denom)
		}
		pkgSummaries = append(pkgSummaries, PackageSummary{Package: name, Summary: *ps})
	}

	tests, zeroKill, redundant := buildTestAggregations(results, testInventory)
	survivors := buildSurvivorsByFunction(results)

	var incidentalFlat []string
	for pkg, names := range incidental {
		for _, n := range names {
			incidentalFlat = append(incidentalFlat, qualifyTestName(pkg, n))
		}
	}
	sort.Strings(incidentalFlat)

	return Report{
		Summary:                 s,
		Packages:                pkgSummaries,
		Tests:                   tests,
		ZeroKillTests:           zeroKill,
		IncidentalCoverageTests: incidentalFlat,
		RedundantTestGroups:     redundant,
		SurvivorsByFunction:     survivors,
		Mutants:                 results,
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

	// Group tests sharing identical non-empty kill-sets.
	byHash := make(map[string][]string)
	for _, ts := range tests {
		if ts.KillCount == 0 {
			continue
		}
		ids := make([]string, len(ts.KilledMutants))
		for i, id := range ts.KilledMutants {
			ids[i] = strconv.Itoa(id)
		}
		h := strings.Join(ids, ",")
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

func qualifyTestName(pkg, name string) string {
	if pkg == "" {
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
	writeLLMLiveSection(&b, r, survivors, src.FuncRanges, readFile)
	writeLLMUncoveredSection(&b, r, uncovered, src.FuncRanges, readFile)
	writeLLMRedundant(&b, r.RedundantTestGroups)
	writeLLMZeroKill(&b, r.ZeroKillTests)
	writeLLMIncidental(&b, r.IncidentalCoverageTests)
	writeLLMTestInventory(&b, r.Tests)
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
// matching the headline finding of arXiv:2506.02954.
func writeLLMPreamble(b *strings.Builder) {
	b.WriteString("## Task\n\n")
	b.WriteString("Your goal is to **raise the mutation score** of this package: write tests that fail when the listed mutations are applied. Line coverage is not the target — a test suite can reach 100% coverage and still kill almost no mutants. Two kinds of offense target follow:\n\n")
	b.WriteString("- **Live mutants** are reached by existing tests but not detected — the fix is to *sharpen an assertion* so the original and mutant diverge.\n")
	b.WriteString("- **Uncovered mutants** are never executed — the fix is to *add a test* that exercises the line; almost any assertion on its result will catch the mutation.\n\n")
	b.WriteString("After editing tests, verify with `kanly --mutant=<ids> <pkg>` (see the iteration block at the end of this report) to re-run only the targeted mutants.\n\n")
}

func writeLLMSummary(b *strings.Builder, s Summary) {
	b.WriteString("## Summary\n\n")
	b.WriteString("| Total | Killed | Survived | Timeout | NotCovered | NotViable | Score |\n")
	b.WriteString("|-------|--------|----------|---------|------------|-----------|-------|\n")
	fmt.Fprintf(b, "| %d | %d | %d | %d | %d | %d | %.1f%% |\n\n",
		s.Total, s.Killed, s.Survived, s.Timeout, s.NotCovered, s.NotViable, s.Score*100)
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
	sort.Slice(out, func(i, j int) bool {
		if out[i].Package != out[j].Package {
			return out[i].Package < out[j].Package
		}
		return out[i].Function < out[j].Function
	})
	return out
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
	"int_literal":         "Assert the exact numeric result so swapping the literal to 0/1/±1 fails.",
	"bool_logic":          "Pick inputs where `&&` and `||` diverge (one true, one false) and assert the boolean result.",
	"bool_not":            "Assert the boolean direction explicitly — removing `!` should flip the test.",
	"bool_literal":        "Assert the exact boolean value (not just truthiness) so `true↔false` is caught.",
	"err_return_nil":      "Drive the function down the error branch and assert `err != nil` (and ideally its kind).",
	"return_zero":         "Assert the returned value, not just `err == nil`; a zero return must fail the test.",
	"call_delete":         "Assert the observable side effect of the call (state change, panic, output), not just that the caller returned.",
	"string_literal":      "Assert the exact string value, not just non-empty or length.",
	"slice_index":         "Use distinct values at adjacent indices and assert the exact element, not just presence.",
	"slice_range":         "Test inputs where shifting the slice bounds by one would change the result.",
	"inc_dec":             "Assert the post-loop counter / iteration count so `++↔--` diverges visibly.",
	"int_compound_assign": "Pick operands where the compound op's direction matters and assert the exact accumulator value.",
	"struct_field_zero":   "Assert the populated field value, not just that the struct is non-nil.",
}

// writeLLMUnkilledGroups renders one offense section (live or uncovered),
// emitting the per-function snippet, the per-mutant line, and an inline
// operator-hint legend that fires once per operator on first occurrence.
func writeLLMUnkilledGroups(b *strings.Builder, groups []mutation.FunctionSurvivors, byID map[int]mutation.Result, funcRanges map[string]map[string]mutation.FuncRange, readFile func(string) ([]byte, error)) {
	seenHints := make(map[string]bool)
	for _, g := range groups {
		fmt.Fprintf(b, "### %s — %s  (%d)\n\n", g.Package, g.Function, len(g.Mutations))

		if pkgRanges, ok := funcRanges[g.Package]; ok {
			if fr, ok := pkgRanges[g.Function]; ok {
				writeLLMSnippet(b, fr, readFile)
			}
		}

		for _, m := range g.Mutations {
			res := byID[m.ID]
			fmt.Fprintf(b, "- **#%d** at %s:%d:%d — `%s` `%s` → `%s`\n",
				m.ID, m.File, m.Line, m.Column, m.OperatorName, m.Original, m.Mutant)
			if hint, ok := operatorHints[m.OperatorName]; ok && !seenHints[m.OperatorName] {
				fmt.Fprintf(b, "  - _Hint (`%s`): %s_\n", m.OperatorName, hint)
				seenHints[m.OperatorName] = true
			}
			writeLLMCoveringTests(b, res)
		}
		b.WriteString("\n")
	}
}

func writeLLMLiveSection(b *strings.Builder, r Report, groups []mutation.FunctionSurvivors, funcRanges map[string]map[string]mutation.FuncRange, readFile func(string) ([]byte, error)) {
	b.WriteString("## Live mutants (covered but not killed)\n\n")
	b.WriteString("_Existing tests reach the mutated line but don't observe the change. Sharpen an assertion so the original and mutant diverge._\n\n")
	if len(groups) == 0 {
		b.WriteString("_None — every covered mutant was killed._\n\n")
		return
	}
	byID := make(map[int]mutation.Result, len(r.Mutants))
	for _, res := range r.Mutants {
		byID[res.Mutation.ID] = res
	}
	writeLLMUnkilledGroups(b, groups, byID, funcRanges, readFile)
}

func writeLLMUncoveredSection(b *strings.Builder, r Report, groups []mutation.FunctionSurvivors, funcRanges map[string]map[string]mutation.FuncRange, readFile func(string) ([]byte, error)) {
	b.WriteString("## Uncovered mutants (no test reaches this line)\n\n")
	b.WriteString("_No test executes this code. Add a new test that drives the path; almost any meaningful assertion on the result will catch the mutation._\n\n")
	if len(groups) == 0 {
		b.WriteString("_None — every mutation site is reached by at least one test._\n\n")
		return
	}
	byID := make(map[int]mutation.Result, len(r.Mutants))
	for _, res := range r.Mutants {
		byID[res.Mutation.ID] = res
	}
	writeLLMUnkilledGroups(b, groups, byID, funcRanges, readFile)
}

// writeLLMNextIteration renders the closing instruction pointing at the
// `--mutant=<ids>` re-verification loop. Skipped when there's nothing to
// iterate on, so a clean-suite artifact ends on the inventory.
func writeLLMNextIteration(b *strings.Builder, survived, notCovered []mutation.FunctionSurvivors) {
	var ids []int
	for _, g := range survived {
		for _, m := range g.Mutations {
			ids = append(ids, m.ID)
		}
	}
	for _, g := range notCovered {
		for _, m := range g.Mutations {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	sort.Ints(ids)
	idStrs := make([]string, len(ids))
	for i, id := range ids {
		idStrs[i] = strconv.Itoa(id)
	}
	b.WriteString("## Next iteration\n\n")
	b.WriteString("After editing tests, re-run only the targeted mutants to verify each is now killed — this skips the compile + baseline + per-test coverage passes for unaffected mutants and is the iterative loop validated in arXiv:2506.02954:\n\n")
	b.WriteString("```\n")
	fmt.Fprintf(b, "kanly --mutant=%s <pkg>\n", strings.Join(idStrs, ","))
	b.WriteString("```\n\n")
	b.WriteString("Any survivor still listed after the re-run is a real assertion gap — feed it back through this artifact and iterate.\n\n")
}

func writeLLMSnippet(b *strings.Builder, fr mutation.FuncRange, readFile func(string) ([]byte, error)) {
	data, err := readFile(fr.File)
	if err != nil {
		fmt.Fprintf(b, "_Source unavailable for %s: %v_\n\n", fr.File, err)
		return
	}
	lines := strings.Split(string(data), "\n")
	if fr.StartLine < 1 || fr.StartLine > len(lines) {
		return
	}
	end := fr.EndLine
	if end > len(lines) {
		end = len(lines)
	}
	fmt.Fprintf(b, "**Source: %s:%d-%d**\n\n", fr.File, fr.StartLine, end)
	b.WriteString("```go\n")
	width := len(strconv.Itoa(end))
	for i := fr.StartLine; i <= end; i++ {
		fmt.Fprintf(b, "%*d  %s\n", width, i, lines[i-1])
	}
	b.WriteString("```\n\n")
}

func writeLLMCoveringTests(b *strings.Builder, res mutation.Result) {
	if res.Status == mutation.StatusNotCovered || len(res.CoveringTests) == 0 {
		b.WriteString("  - Covering tests that did NOT kill: _none — mutation site is not exercised by any test_\n")
		return
	}
	killers := make(map[string]struct{}, len(res.KillingTests))
	for _, t := range res.KillingTests {
		killers[t] = struct{}{}
	}
	var coveringNonKillers []string
	for _, t := range res.CoveringTests {
		if _, killed := killers[t]; killed {
			continue
		}
		coveringNonKillers = append(coveringNonKillers, t)
	}
	if len(coveringNonKillers) == 0 {
		b.WriteString("  - Covering tests that did NOT kill: _none_\n")
		return
	}
	sort.Strings(coveringNonKillers)
	qualified := make([]string, len(coveringNonKillers))
	for i, name := range coveringNonKillers {
		qualified[i] = qualifyTestName(res.Mutation.Package, name)
	}
	fmt.Fprintf(b, "  - Covering tests that did NOT kill: %s\n", strings.Join(backtickAll(qualified), ", "))
}

func backtickAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = "`" + s + "`"
	}
	return out
}

func writeLLMRedundant(b *strings.Builder, groups [][]string) {
	b.WriteString("## Redundant test groups (consolidation targets)\n\n")
	if len(groups) == 0 {
		b.WriteString("_None — no two tests share an identical kill set._\n\n")
		return
	}
	b.WriteString("Each group's tests kill exactly the same mutants; keep one and delete the rest unless they assert different behaviour.\n\n")
	for _, g := range groups {
		fmt.Fprintf(b, "- %s\n", strings.Join(backtickAll(g), ", "))
	}
	b.WriteString("\n")
}

func writeLLMZeroKill(b *strings.Builder, tests []string) {
	b.WriteString("## Zero-kill tests (deletion candidates)\n\n")
	if len(tests) == 0 {
		b.WriteString("_None — every test killed at least one mutant._\n\n")
		return
	}
	b.WriteString("These tests killed no mutants within the current scope. Consider deleting or rewriting them to target a surviving mutant above.\n\n")
	for _, t := range tests {
		fmt.Fprintf(b, "- `%s`\n", t)
	}
	b.WriteString("\n")
}

// writeLLMIncidental renders the incidental-coverage section. Silent on empty
// input so full / non-scoped runs render byte-identically to prior versions.
// The wording emphasises that these tests still belong to the suite — kanly
// only excluded them from this scoped run's per-mutant set because their
// statement coverage of a scoped line looked like helper / setup traffic
// rather than an assertion. They are NOT deletion candidates.
func writeLLMIncidental(b *strings.Builder, tests []string) {
	if len(tests) == 0 {
		return
	}
	b.WriteString("## Incidental coverage (excluded from scope analysis)\n\n")
	b.WriteString("These tests touched a scoped line via shared setup or a helper call but were not run against the targeted mutants — their single hit on the line, alongside another test hitting it harder, indicates the line is not what they're asserting on. They still run in the full suite; this section only documents the exclusion so they don't appear as zero-kill noise.\n\n")
	for _, t := range tests {
		fmt.Fprintf(b, "- `%s`\n", t)
	}
	b.WriteString("\n")
}

func writeLLMTestInventory(b *strings.Builder, tests []mutation.TestStats) {
	b.WriteString("## Test inventory\n\n")
	if len(tests) == 0 {
		b.WriteString("_No tests in scope._\n\n")
		return
	}
	b.WriteString("| Test | KillCount | Killed mutants |\n")
	b.WriteString("|------|-----------|----------------|\n")
	for _, ts := range tests {
		killed := "_none_"
		if len(ts.KilledMutants) > 0 {
			ids := make([]string, len(ts.KilledMutants))
			for i, id := range ts.KilledMutants {
				ids[i] = "#" + strconv.Itoa(id)
			}
			killed = strings.Join(ids, ", ")
		}
		fmt.Fprintf(b, "| `%s` | %d | %s |\n", qualifyTestName(ts.Package, ts.Name), ts.KillCount, killed)
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
	// Surviving mutants first — they are the actionable signal.
	for _, m := range r.Mutants {
		if m.Status == mutation.StatusSurvived {
			if _, err := fmt.Fprintf(w, "%s:%d:%d [%s] %s→%s\n",
				m.Mutation.File, m.Mutation.Line, m.Mutation.Column,
				m.Mutation.OperatorName, m.Mutation.Original, m.Mutation.Mutant,
			); err != nil {
				return err
			}
		}
	}
	for _, ps := range r.Packages {
		if _, err := fmt.Fprintf(w, "Package: %s | Total: %d | Killed: %d | Survived: %d | Score: %.1f%%\n",
			ps.Package, ps.Total, ps.Killed, ps.Survived, ps.Score*100,
		); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "\nTotal: %d | Killed: %d | Survived: %d | Timeout: %d | Score: %.1f%%\n",
		r.Summary.Total, r.Summary.Killed, r.Summary.Survived, r.Summary.Timeout,
		r.Summary.Score*100,
	); err != nil {
		return err
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
		if _, err := fmt.Fprintln(w, "\nTests that killed nothing:"); err != nil {
			return err
		}
		for _, n := range r.ZeroKillTests {
			if _, err := fmt.Fprintf(w, "  %s\n", n); err != nil {
				return err
			}
		}
	}

	if len(r.IncidentalCoverageTests) > 0 {
		if _, err := fmt.Fprintln(w, "\nIncidental coverage (excluded from kill analysis):"); err != nil {
			return err
		}
		for _, n := range r.IncidentalCoverageTests {
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
