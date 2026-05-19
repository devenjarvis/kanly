package operators

import (
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

func TestStructFieldZeroFindsKeyedInitializers(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/structfieldpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, StructFieldZero{}.Find(f, pkg.TypesInfo)...)
	}

	// MakeKeyed contributes 5; NestedStruct's outer Label contributes 1.
	want := 6
	if len(candidates) != want {
		t.Fatalf("expected %d candidates, got %d:\n%+v", want, len(candidates), candidates)
	}
	for _, c := range candidates {
		if c.Original != "value" {
			t.Errorf("expected Original %q, got %q", "value", c.Original)
		}
		switch c.Mutant {
		case "0", `""`, "nil":
			// OK
		default:
			t.Errorf("unexpected Mutant %q", c.Mutant)
		}
	}
}

func TestStructFieldZeroSkipsExcludedShapes(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/structfieldpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	rew, err := schema.Rewrite(pkg, []mutation.Operator{StructFieldZero{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for path, src := range rew.Files {
		// SkippedShapes' BasicLits and BinaryExpr must survive untouched.
		if !strings.Contains(src, `Name:  "literal"`) {
			t.Errorf("%s: SkippedShapes BasicLit must survive rewrite:\n%s", path, src)
		}
		if !strings.Contains(src, "Count: a + b") {
			t.Errorf("%s: SkippedShapes BinaryExpr must survive rewrite:\n%s", path, src)
		}
		// PositionalLit must survive entirely — Elts aren't KeyValueExpr.
		if !strings.Contains(src, `Item{"n", 1, "t", nil, nil}`) {
			t.Errorf("%s: positional struct literal must survive rewrite:\n%s", path, src)
		}
		// SliceLit / MapLit must survive entirely — non-struct composite types.
		if !strings.Contains(src, "[]int{1, 2, 3}") {
			t.Errorf("%s: slice composite literal must survive rewrite:\n%s", path, src)
		}
		if !strings.Contains(src, `map[string]int{"a": 1}`) {
			t.Errorf("%s: map composite literal must survive rewrite:\n%s", path, src)
		}
		// NestedStruct's inner Item{Name: "x"} must remain unwrapped — its type
		// is a non-nilable struct so the outer KV is not a candidate.
		if !strings.Contains(src, `Inner: Item{Name: "x"}`) {
			t.Errorf("%s: nested struct literal must remain unwrapped:\n%s", path, src)
		}
	}
}

func TestStructFieldZeroRewriteEmitsMutRetZero(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/structfieldpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	rew, err := schema.Rewrite(pkg, []mutation.Operator{StructFieldZero{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for path, src := range rew.Files {
		count := strings.Count(src, "__cMutRetZero(")
		// 5 from MakeKeyed + 1 from NestedStruct.
		if count != 6 {
			t.Errorf("%s: expected 6 __cMutRetZero call sites, got %d:\n%s", path, count, src)
		}
	}
	if !strings.Contains(rew.Dispatcher, "func __cMutRetZero[T any](") {
		t.Errorf("dispatcher missing __cMutRetZero:\n%s", rew.Dispatcher)
	}
}

func TestStructFieldZeroAndReturnZeroCoexist(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/structfieldpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	// Both operators share DispatcherKey "return_zero", so the dispatcher
	// must include __cMutRetZero exactly once even when both are active.
	rew, err := schema.Rewrite(pkg, []mutation.Operator{ReturnZero{}, StructFieldZero{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}
	if strings.Count(rew.Dispatcher, "func __cMutRetZero[T any](") != 1 {
		t.Errorf("expected exactly one __cMutRetZero declaration:\n%s", rew.Dispatcher)
	}
}
