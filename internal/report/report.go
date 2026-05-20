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
	Scope               string                       `json:"scope,omitempty"`
	Summary             Summary                      `json:"summary"`
	Packages            []PackageSummary             `json:"packages"`
	Tests               []mutation.TestStats         `json:"tests"`
	ZeroKillTests       []string                     `json:"zero_kill_tests"`
	RedundantTestGroups [][]string                   `json:"redundant_test_groups"`
	SurvivorsByFunction []mutation.FunctionSurvivors `json:"survivors_by_function"`
	Mutants             []mutation.Result            `json:"mutants"`
}

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

	return Report{
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

	writeLLMSummary(&b, r.Summary)
	writeLLMSurvivors(&b, r, src.FuncRanges, readFile)
	writeLLMRedundant(&b, r.RedundantTestGroups)
	writeLLMZeroKill(&b, r.ZeroKillTests)
	writeLLMTestInventory(&b, r.Tests)

	out := b.String()
	if _, err := io.WriteString(w, out); err != nil {
		return err
	}
	if len(out) > llmWarnSize {
		fmt.Fprintf(os.Stderr, "kanly: llm output is %d bytes; consider scoping with --diff, --tests, or pkg:func\n", len(out))
	}
	return nil
}

func writeLLMSummary(b *strings.Builder, s Summary) {
	b.WriteString("## Summary\n\n")
	b.WriteString("| Total | Killed | Survived | Timeout | NotCovered | NotViable | Score |\n")
	b.WriteString("|-------|--------|----------|---------|------------|-----------|-------|\n")
	fmt.Fprintf(b, "| %d | %d | %d | %d | %d | %d | %.1f%% |\n\n",
		s.Total, s.Killed, s.Survived, s.Timeout, s.NotCovered, s.NotViable, s.Score*100)
}

// unkilledByFunction groups survived AND not-covered mutants by (package,
// function) — both are offense targets. The report.SurvivorsByFunction only
// covers status=survived, so we re-bucket from r.Mutants here.
func unkilledByFunction(results []mutation.Result) []mutation.FunctionSurvivors {
	type key struct{ pkg, fn string }
	groups := make(map[key]*mutation.FunctionSurvivors)
	for _, r := range results {
		if r.Status != mutation.StatusSurvived && r.Status != mutation.StatusNotCovered {
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

func writeLLMSurvivors(b *strings.Builder, r Report, funcRanges map[string]map[string]mutation.FuncRange, readFile func(string) ([]byte, error)) {
	b.WriteString("## Surviving mutants (offense targets)\n\n")

	groups := unkilledByFunction(r.Mutants)
	if len(groups) == 0 {
		b.WriteString("_None — every mutant was killed._\n\n")
		return
	}

	// Index results by mutation ID so each group entry can look up its
	// CoveringTests/KillingTests without re-scanning r.Mutants.
	byID := make(map[int]mutation.Result, len(r.Mutants))
	for _, res := range r.Mutants {
		byID[res.Mutation.ID] = res
	}

	for _, g := range groups {
		fmt.Fprintf(b, "### %s — %s  (%d unkilled)\n\n", g.Package, g.Function, len(g.Mutations))

		// Source snippet, if we have a range for this function.
		if pkgRanges, ok := funcRanges[g.Package]; ok {
			if fr, ok := pkgRanges[g.Function]; ok {
				writeLLMSnippet(b, fr, readFile)
			}
		}

		for _, m := range g.Mutations {
			res := byID[m.ID]
			status := string(res.Status)
			fmt.Fprintf(b, "- **#%d** at %s:%d:%d — `%s` `%s` → `%s` _(%s)_\n",
				m.ID, m.File, m.Line, m.Column, m.OperatorName, m.Original, m.Mutant, status)
			writeLLMCoveringTests(b, res)
		}
		b.WriteString("\n")
	}
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
