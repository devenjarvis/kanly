package mutation

import (
	"time"
)

type Status string

const (
	StatusKilled     Status = "killed"
	StatusSurvived   Status = "survived"
	StatusTimeout    Status = "timeout"
	StatusNotViable  Status = "not_viable"
	StatusNotCovered Status = "not_covered"
)

type Mutation struct {
	ID           int    `json:"mutation_id"`
	Package      string `json:"package"`
	File         string `json:"file"`
	Line         int    `json:"line"`
	Column       int    `json:"column"`
	Function     string `json:"function"`
	OperatorName string `json:"operator"`
	Original     string `json:"original"`
	Mutant       string `json:"mutant"`
	// PackageInit marks a mutation in a package-level variable initializer
	// (outside any function body). It runs once per test process, but Go's
	// coverage instrumentation never attributes it to a test.
	PackageInit bool `json:"-"`
}

type Result struct {
	Mutation      Mutation      `json:"mutation"`
	Status        Status        `json:"status"`
	KillingTests  []string      `json:"killing_tests"`
	CoveringTests []string      `json:"covering_tests"`
	Duration      time.Duration `json:"duration_ns"`
	// Note explains a status that changed when the mutant was re-run alone
	// to confirm it (see --verify-kills); empty otherwise.
	Note string `json:"note,omitempty"`
}

// FuncRange identifies a top-level function's source range, used by the LLM
// renderer to slice out the enclosing function for each surviving mutant.
type FuncRange struct {
	File      string
	StartLine int
	EndLine   int
}

// TestStats summarises a single test's mutation-killing behaviour within a package.
type TestStats struct {
	Package       string `json:"package"`
	Name          string `json:"name"`
	KillCount     int    `json:"kill_count"`
	KilledMutants []int  `json:"killed_mutants"`
}

// FunctionSurvivors groups surviving mutations by their enclosing function.
type FunctionSurvivors struct {
	Package   string     `json:"package"`
	Function  string     `json:"function"`
	Mutations []Mutation `json:"mutations"`
}
