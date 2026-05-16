package report

import (
	"encoding/json"
	"fmt"
	"io"

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

type Report struct {
	Summary Summary          `json:"summary"`
	Mutants []mutation.Result `json:"mutants"`
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
	denominator := s.Total - s.NotCovered
	if denominator > 0 {
		s.Score = float64(s.Killed) / float64(denominator)
	}
	return Report{Summary: s, Mutants: results}
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
