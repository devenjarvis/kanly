package schema_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/operators"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

func TestRewriteHonoursIgnoreDirectives(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/ignorepkg"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ops := []mutation.Operator{operators.IntArith{}, operators.IntCmpBoundary{}}
	rew, err := schema.Rewrite(pkg, ops, nil)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	var got []string
	for _, m := range rew.Mutations {
		got = append(got, m.Function+" "+m.OperatorName)
	}
	want := []string{"Plain int_arith", "OtherOp int_cmp_boundary"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mutations = %v, want %v", got, want)
	}
	// Trailing, Above, Both ×2, and Whole's *, / and >.
	if rew.Ignored != 7 {
		t.Errorf("Ignored = %d, want 7", rew.Ignored)
	}
	for file := range rew.Files {
		if strings.HasSuffix(file, "generated.go") {
			t.Error("generated file was rewritten")
		}
	}
}

// Narrowing by operator must not renumber the mutants that remain.
func TestRewriteWithOperatorsKeepsIDs(t *testing.T) {
	// Rewrite modifies the package's AST, so each rewrite loads afresh.
	load := func() *source.Package {
		pkg, err := source.Load(relDir(t, "testdata/ignorepkg"))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		return pkg
	}
	ops := []mutation.Operator{operators.IntArith{}, operators.IntCmpBoundary{}}
	all, err := schema.Rewrite(load(), ops, nil)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	narrow, err := schema.RewriteWith(load(), ops, schema.Options{Operators: map[string]bool{"int_cmp_boundary": true}})
	if err != nil {
		t.Fatalf("RewriteWith: %v", err)
	}
	var want []mutation.Mutation
	for _, m := range all.Mutations {
		if m.OperatorName == "int_cmp_boundary" {
			want = append(want, m)
		}
	}
	if !reflect.DeepEqual(narrow.Mutations, want) {
		t.Errorf("narrowed mutations = %v, want %v", narrow.Mutations, want)
	}
}

func TestRewriteRejectsUnknownIgnoredOperator(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/ignorebad"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = schema.Rewrite(pkg, []mutation.Operator{operators.IntArith{}}, nil)
	if err == nil || !strings.Contains(err.Error(), `unknown operator "int_arth"`) || !strings.Contains(err.Error(), "bad.go:4") {
		t.Errorf("Rewrite error = %v, want an unknown-operator error at bad.go:4", err)
	}
}
