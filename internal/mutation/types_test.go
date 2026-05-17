package mutation_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
)

func TestResultJSONFieldNames(t *testing.T) {
	r := mutation.Result{
		Mutation: mutation.Mutation{
			ID:           1,
			File:         "foo.go",
			Line:         10,
			Column:       5,
			OperatorName: "int_arith",
			Original:     "+",
			Mutant:       "-",
		},
		Status:       mutation.StatusKilled,
		KillingTests: []string{"TestFoo"},
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)

	for _, field := range []string{"mutation_id", "file", "line", "column", "operator", "original", "mutant", "status", "killing_tests"} {
		if !strings.Contains(s, `"`+field+`"`) {
			t.Errorf("JSON missing field %q: %s", field, s)
		}
	}
}
