package report_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/report"
)

func TestWriteSARIF(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(cwd, "pkg", "calc.go")
	mk := func(id, line int, status mutation.Status) mutation.Result {
		return mutation.Result{
			Mutation:      mutation.Mutation{ID: id, Package: "example.com/pkg", File: file, Line: line, Column: 9, Function: "Add", OperatorName: "int_arith", Original: "+", Mutant: "-"},
			Status:        status,
			CoveringTests: []string{"TestAdd"},
		}
	}
	r := report.Build([]mutation.Result{
		mk(1, 5, mutation.StatusKilled),
		mk(2, 6, mutation.StatusSurvived),
		mk(3, 7, mutation.StatusNotCovered),
	}, nil)
	r.Errors = []report.PackageError{{Package: "example.com/broken", Stage: "compile", Error: "boom\nmore"}}

	var buf bytes.Buffer
	if err := report.WriteSARIF(&buf, r, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	var log struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name    string `json:"name"`
					Version string `json:"version"`
					Rules   []struct {
						ID string `json:"id"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Invocations []struct {
				ExecutionSuccessful bool `json:"executionSuccessful"`
				Notifications       []struct {
					Level   string `json:"level"`
					Message struct {
						Text string `json:"text"`
					} `json:"message"`
				} `json:"toolExecutionNotifications"`
			} `json:"invocations"`
			Results []struct {
				RuleID  string `json:"ruleId"`
				Level   string `json:"level"`
				Message struct {
					Text string `json:"text"`
				} `json:"message"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
				PartialFingerprints map[string]string `json:"partialFingerprints"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("parse SARIF: %v\n%s", err, buf.String())
	}
	if log.Version != "2.1.0" || len(log.Runs) != 1 {
		t.Fatalf("version %q with %d runs, want 2.1.0 with 1", log.Version, len(log.Runs))
	}
	run := log.Runs[0]
	if run.Tool.Driver.Name != "kanly" || run.Tool.Driver.Version != "v1.2.3" {
		t.Errorf("driver = %+v", run.Tool.Driver)
	}
	if len(run.Tool.Driver.Rules) != 1 || run.Tool.Driver.Rules[0].ID != "int_arith" {
		t.Errorf("rules = %+v, want [int_arith]", run.Tool.Driver.Rules)
	}
	if len(run.Results) != 2 {
		t.Fatalf("results = %d, want 2 (survived and not covered; killed omitted)", len(run.Results))
	}
	survived, uncovered := run.Results[0], run.Results[1]
	if !strings.Contains(survived.Message.Text, "Mutant 2 survived") || !strings.Contains(survived.Message.Text, "TestAdd") {
		t.Errorf("survived message = %q", survived.Message.Text)
	}
	if !strings.Contains(uncovered.Message.Text, "Mutant 3 is not covered") {
		t.Errorf("not-covered message = %q", uncovered.Message.Text)
	}
	loc := survived.Locations[0].PhysicalLocation
	if loc.ArtifactLocation.URI != "pkg/calc.go" || loc.Region.StartLine != 6 {
		t.Errorf("location = %+v, want pkg/calc.go:6", loc)
	}
	if survived.PartialFingerprints["kanlyMutant/v1"] == uncovered.PartialFingerprints["kanlyMutant/v1"] {
		t.Error("distinct mutants share a fingerprint")
	}
	inv := run.Invocations[0]
	if inv.ExecutionSuccessful || len(inv.Notifications) != 1 || !strings.Contains(inv.Notifications[0].Message.Text, "example.com/broken was not tested (compile): boom") {
		t.Errorf("invocation = %+v", inv)
	}
}
