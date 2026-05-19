package operators

import (
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

func TestBoolLiteralFindsTypedBoolLiterals(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/boollitpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, BoolLiteral{}.Find(f, pkg.TypesInfo)...)
	}

	// Eligible literals (typed-bool context, not in a const/skipped position):
	//   ReturnTrue:        true             → 1
	//   ReturnFalse:       false            → 1
	//   AssignTypedBool:   true (typed var) → 1
	//   ShortDeclTrue:     true (:= )       → 1
	//   CallArgTypedBool:  false (arg)      → 1
	// Skipped: ConstBool (const-decl), IfCondLiteral (untyped if-cond),
	// NamedAssignment (MyBool context).
	want := 5
	if len(candidates) != want {
		t.Fatalf("expected %d candidates, got %d:\n%+v", want, len(candidates), candidates)
	}

	var trues, falses int
	for _, c := range candidates {
		switch c.Original {
		case "true":
			if c.Mutant != "false" {
				t.Errorf("expected true→false, got true→%s", c.Mutant)
			}
			trues++
		case "false":
			if c.Mutant != "true" {
				t.Errorf("expected false→true, got false→%s", c.Mutant)
			}
			falses++
		default:
			t.Errorf("unexpected Original %q", c.Original)
		}
	}
	if trues != 3 {
		t.Errorf("expected 3 true→false candidates, got %d", trues)
	}
	if falses != 2 {
		t.Errorf("expected 2 false→true candidates, got %d", falses)
	}
}

func TestBoolLiteralSkipsForbiddenContexts(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/boollitpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	rew, err := schema.Rewrite(pkg, []mutation.Operator{BoolLiteral{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}
	for path, src := range rew.Files {
		// Const-decl initializer must remain a constant literal.
		if !strings.Contains(src, "const ConstBool = true") {
			t.Errorf("%s: const initializer must survive rewrite:\n%s", path, src)
		}
		// Named-bool assignment must not be wrapped — the helper returns plain
		// bool and would not type-check in MyBool context.
		if !strings.Contains(src, "var v MyBool = true") {
			t.Errorf("%s: named-bool initializer must survive rewrite:\n%s", path, src)
		}
		// `if true` keeps the literal untyped; strict-identity excludes it.
		if !strings.Contains(src, "if true {") {
			t.Errorf("%s: untyped if-cond literal must survive rewrite:\n%s", path, src)
		}
	}
}

func TestBoolLiteralRewriteEmitsMutBoolLit(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/boollitpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	rew, err := schema.Rewrite(pkg, []mutation.Operator{BoolLiteral{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}
	for path, src := range rew.Files {
		count := strings.Count(src, "__cMutBoolLit(")
		if count != 5 {
			t.Errorf("%s: expected 5 __cMutBoolLit call sites, got %d:\n%s", path, count, src)
		}
	}
	if !strings.Contains(rew.Dispatcher, "func __cMutBoolLit(") {
		t.Errorf("dispatcher missing __cMutBoolLit:\n%s", rew.Dispatcher)
	}
}
