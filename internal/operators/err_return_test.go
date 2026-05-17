package operators

import (
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

func TestErrReturnFindsAllReturnSiteErrors(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/errpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, ErrReturnNil{}.Find(f, pkg.TypesInfo)...)
	}

	if len(candidates) != 5 {
		t.Fatalf("expected 5 candidates (ReturnErr+ReturnTwo+2×ReturnTwoErrors+ReturnCall), got %d", len(candidates))
	}
	for _, c := range candidates {
		if c.Original != "err" || c.Mutant != "nil" {
			t.Errorf("expected err→nil, got %q→%q", c.Original, c.Mutant)
		}
	}
}

func TestErrReturnSkipsLiteralNilAndNonErrors(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/errpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{ErrReturnNil{}})
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for _, src := range rew.Files {
		// Literal nil returns must be left untouched.
		if !strings.Contains(src, "return nil") {
			t.Errorf("expected literal `return nil` to survive rewrite:\n%s", src)
		}
		// MyErr return must not be wrapped — concrete error type is excluded.
		if !strings.Contains(src, "return MyErr{}") {
			t.Errorf("expected named-error return to survive rewrite:\n%s", src)
		}
		// Non-error int returns must be untouched.
		if !strings.Contains(src, "return 42") {
			t.Errorf("expected non-error int return to survive rewrite:\n%s", src)
		}
	}
}

func TestErrReturnRewriteEmitsMutErr(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/errpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{ErrReturnNil{}})
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for path, src := range rew.Files {
		count := strings.Count(src, "__cMutErr(")
		if count != 5 {
			t.Errorf("%s: expected 5 __cMutErr calls, got %d:\n%s", path, count, src)
		}
		// Multi-value return must preserve the non-error operand unchanged.
		if !strings.Contains(src, "return 0, __cMutErr(err") {
			t.Errorf("%s: expected `return 0, __cMutErr(err...)` in ReturnTwo:\n%s", path, src)
		}
		// Call expression must be wrapped.
		if !strings.Contains(src, `__cMutErr(errors.New("x")`) {
			t.Errorf("%s: expected errors.New(\"x\") to be wrapped:\n%s", path, src)
		}
	}
}
