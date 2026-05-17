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
	File         string `json:"file"`
	Line         int    `json:"line"`
	Column       int    `json:"column"`
	OperatorName string `json:"operator"`
	Original     string `json:"original"`
	Mutant       string `json:"mutant"`
}

type Result struct {
	Mutation     Mutation      `json:"mutation"`
	Status       Status        `json:"status"`
	KillingTests []string      `json:"killing_tests"`
	Duration     time.Duration `json:"duration_ns"`
}
