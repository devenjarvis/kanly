package report

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// SARIF 2.1.0 output, for code-scanning tools such as GitHub's, which show
// each result as an annotation on the line. Survived and not-covered
// mutants are results; packages that could not be tested are tool
// execution notifications. Paths are relative to the working directory, so
// run from the repository root when uploading to code scanning.

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool        sarifTool         `json:"tool"`
	Invocations []sarifInvocation `json:"invocations"`
	Results     []sarifResult     `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Version        string      `json:"version,omitempty"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string       `json:"id"`
	ShortDescription sarifMessage `json:"shortDescription"`
	Help             sarifMessage `json:"help"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifInvocation struct {
	ExecutionSuccessful bool                `json:"executionSuccessful"`
	Notifications       []sarifNotification `json:"toolExecutionNotifications,omitempty"`
}

type sarifNotification struct {
	Level   string       `json:"level"`
	Message sarifMessage `json:"message"`
}

type sarifResult struct {
	RuleID              string            `json:"ruleId"`
	Level               string            `json:"level"`
	Message             sarifMessage      `json:"message"`
	Locations           []sarifLocation   `json:"locations"`
	PartialFingerprints map[string]string `json:"partialFingerprints"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
}

// WriteSARIF renders r as a SARIF 2.1.0 log. toolVersion is Kanly's version.
func WriteSARIF(w io.Writer, r Report, toolVersion string) error {
	run := sarifRun{
		Tool: sarifTool{Driver: sarifDriver{
			Name:           "kanly",
			InformationURI: "https://github.com/devenjarvis/kanly",
			Version:        toolVersion,
		}},
		Invocations: []sarifInvocation{{ExecutionSuccessful: len(r.Errors) == 0}},
		Results:     []sarifResult{},
	}
	for _, e := range r.Errors {
		run.Invocations[0].Notifications = append(run.Invocations[0].Notifications, sarifNotification{
			Level:   "error",
			Message: sarifMessage{Text: fmt.Sprintf("%s was not tested (%s): %s", e.Package, e.Stage, firstLine(e.Error))},
		})
	}

	usedRules := make(map[string]bool)
	occurrences := make(map[string]int)
	for _, m := range sortedByLocation(r.Mutants, mutation.StatusSurvived, mutation.StatusNotCovered) {
		mut := m.Mutation
		usedRules[mut.OperatorName] = true
		change := fmt.Sprintf("replacing `%s` with `%s` (%s)", mut.Original, mut.Mutant, mut.OperatorName)
		var text string
		if m.Status == mutation.StatusSurvived {
			text = fmt.Sprintf("Mutant %d survived: %s made no test fail.", mut.ID, change)
			if len(m.CoveringTests) > 0 {
				text += " Tests that ran it: " + strings.Join(m.CoveringTests, ", ") + "."
			}
		} else {
			text = fmt.Sprintf("Mutant %d is not covered: no test runs this code, so %s goes undetected.", mut.ID, change)
		}
		if m.Note != "" {
			text += " " + m.Note + "."
		}
		if hint := operatorHints[mut.OperatorName]; hint != "" {
			text += " " + hint
		}
		// The fingerprint survives edits that move the line: it names the
		// change, not its position, numbering repeats within a function.
		key := strings.Join([]string{mut.Package, mut.Function, mut.OperatorName, mut.Original, mut.Mutant}, "|")
		occurrences[key]++
		run.Results = append(run.Results, sarifResult{
			RuleID:  mut.OperatorName,
			Level:   "warning",
			Message: sarifMessage{Text: text},
			Locations: []sarifLocation{{PhysicalLocation: sarifPhysicalLocation{
				ArtifactLocation: sarifArtifactLocation{URI: filepath.ToSlash(relPath(mut.File)), URIBaseID: "%SRCROOT%"},
				Region:           sarifRegion{StartLine: mut.Line, StartColumn: mut.Column},
			}}},
			PartialFingerprints: map[string]string{
				"kanlyMutant/v1": fmt.Sprintf("%s|%d|%s", key, occurrences[key], m.Status),
			},
		})
	}

	rules := make([]string, 0, len(usedRules))
	for id := range usedRules {
		rules = append(rules, id)
	}
	sort.Strings(rules)
	for _, id := range rules {
		help := operatorHints[id]
		if help == "" {
			help = "Add or sharpen a test so that the mutated code makes it fail."
		}
		run.Tool.Driver.Rules = append(run.Tool.Driver.Rules, sarifRule{
			ID:               id,
			ShortDescription: sarifMessage{Text: "Undetected " + id + " mutant"},
			Help:             sarifMessage{Text: help},
		})
	}
	if run.Tool.Driver.Rules == nil {
		run.Tool.Driver.Rules = []sarifRule{}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs:    []sarifRun{run},
	})
}

// firstLine returns the first line of an error message, joined with the
// next non-blank line when it only introduces what follows ("...:").
func firstLine(s string) string {
	line, rest, _ := strings.Cut(s, "\n")
	if !strings.HasSuffix(line, ":") {
		return line
	}
	for _, next := range strings.Split(rest, "\n") {
		if next = strings.TrimSpace(next); next != "" {
			return line + " " + next
		}
	}
	return line
}
