package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/devenjarvis/cauldron/internal/mutation"
)

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
	Summary  Summary          `json:"summary"`
	Packages []PackageSummary `json:"packages"`
	Mutants  []mutation.Result `json:"mutants"`
}

func Build(results []mutation.Result) Report {
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

	return Report{Summary: s, Packages: pkgSummaries, Mutants: results}
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
	_, err := fmt.Fprintf(w, "\nTotal: %d | Killed: %d | Survived: %d | Timeout: %d | Score: %.1f%%\n",
		r.Summary.Total, r.Summary.Killed, r.Summary.Survived, r.Summary.Timeout,
		r.Summary.Score*100,
	)
	return err
}
