package operators

import (
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

func TestCallDeleteFindsAllVoidCallStmts(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/calldelete"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, CallDelete{}.Find(f, pkg.TypesInfo)...)
	}

	// Expected positives (10):
	//   BareCall/MethodCall/GenericCall/IfInit/CloseCh/DeleteEntry/ClearSlice/RecoverCall/
	//   NestedCall/MixedOps
	if len(candidates) != 10 {
		t.Fatalf("expected 10 candidates, got %d", len(candidates))
	}
	for _, c := range candidates {
		if c.Mutant != "" {
			t.Errorf("expected Mutant to be empty, got %q", c.Mutant)
		}
		if !strings.HasSuffix(c.Original, "(...)") {
			t.Errorf("expected Original to end in '(...)', got %q", c.Original)
		}
	}
}

func TestCallDeleteSkipsExcludedBuiltins(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/calldelete"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, CallDelete{}.Find(f, pkg.TypesInfo)...)
	}

	// panic / print / println must never appear as candidate callees.
	for _, c := range candidates {
		switch c.Original {
		case "panic(...)", "print(...)", "println(...)":
			t.Errorf("excluded builtin slipped through: %q", c.Original)
		}
	}
}

func TestCallDeleteSkipsDeferGoAssign(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/calldelete"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{CallDelete{}})
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for path, src := range rew.Files {
		// Defer and go statements must survive untouched.
		if !strings.Contains(src, "defer helper()") {
			t.Errorf("%s: expected `defer helper()` to survive rewrite:\n%s", path, src)
		}
		if !strings.Contains(src, "go helper()") {
			t.Errorf("%s: expected `go helper()` to survive rewrite:\n%s", path, src)
		}
		// Assigned-result calls must survive untouched.
		if !strings.Contains(src, "_ = sideEffect()") {
			t.Errorf("%s: expected `_ = sideEffect()` to survive rewrite:\n%s", path, src)
		}
		// if-condition calls must survive.
		if !strings.Contains(src, "if boolFn()") {
			t.Errorf("%s: expected `if boolFn()` to survive rewrite:\n%s", path, src)
		}
		// Excluded builtins must survive in their original form.
		if !strings.Contains(src, `panic("excluded")`) {
			t.Errorf("%s: expected `panic(\"excluded\")` to survive rewrite:\n%s", path, src)
		}
		if !strings.Contains(src, `print("excluded")`) {
			t.Errorf("%s: expected `print(\"excluded\")` to survive rewrite:\n%s", path, src)
		}
		if !strings.Contains(src, `println("excluded")`) {
			t.Errorf("%s: expected `println(\"excluded\")` to survive rewrite:\n%s", path, src)
		}
	}
}

func TestCallDeleteRewriteEmitsCallSkip(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/calldelete"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{CallDelete{}})
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for path, src := range rew.Files {
		count := strings.Count(src, "__cMutCallSkip(")
		if count != 10 {
			t.Errorf("%s: expected 10 __cMutCallSkip calls, got %d:\n%s", path, count, src)
		}
		// Spot-check that the wrapped method call body is preserved inside the closure.
		if !strings.Contains(src, "r.method()") {
			t.Errorf("%s: expected `r.method()` to be preserved inside closure:\n%s", path, src)
		}
		// Spot-check generic instantiation is preserved inside the closure.
		if !strings.Contains(src, "genericFn[int](1)") {
			t.Errorf("%s: expected generic instantiation to be preserved:\n%s", path, src)
		}
		// Spot-check builtins-we-keep are preserved inside their closures.
		if !strings.Contains(src, "close(ch)") {
			t.Errorf("%s: expected `close(ch)` preserved inside closure:\n%s", path, src)
		}
		if !strings.Contains(src, `delete(m, "a")`) {
			t.Errorf("%s: expected `delete(m, \"a\")` preserved inside closure:\n%s", path, src)
		}
		if !strings.Contains(src, "clear(s)") {
			t.Errorf("%s: expected `clear(s)` preserved inside closure:\n%s", path, src)
		}
		if !strings.Contains(src, "recover()") {
			t.Errorf("%s: expected `recover()` preserved inside closure:\n%s", path, src)
		}
	}
}
