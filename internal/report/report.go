package report

import (
	"encoding/json"
	"fmt"
	"io"
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

func WriteJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func WriteText(w io.Writer, r Report) error {
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
