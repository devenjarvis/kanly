package operators

import (
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

func TestStringLiteralFindsMutableLiterals(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/stringpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, StringLiteral{}.Find(f, pkg.TypesInfo)...)
	}

	// Expected mutable sites: "hello", "x=%d", `raw`. Skipped: empty literals,
	// struct tag `json:"f"`, const "alice", import "fmt".
	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(candidates))
	}
	for _, c := range candidates {
		if c.Mutant != `""` {
			t.Errorf("expected Mutant=\"\", got %q", c.Mutant)
		}
		if c.Original == `""` || c.Original == "``" {
			t.Errorf("empty literal should have been skipped, got Original=%q", c.Original)
		}
	}

	got := map[string]bool{}
	for _, c := range candidates {
		got[c.Original] = true
	}
	for _, want := range []string{`"hello"`, `"x=%d"`, "`raw`"} {
		if !got[want] {
			t.Errorf("missing expected candidate %s", want)
		}
	}
}

func TestStringLiteralSkipsForbiddenContexts(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/stringpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{StringLiteral{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for path, src := range rew.Files {
		// Import paths must remain literal.
		if !strings.Contains(src, `import "fmt"`) && !strings.Contains(src, `"fmt"`) {
			t.Errorf("%s: import path must survive rewrite:\n%s", path, src)
		}
		if strings.Contains(src, `__cMutString("fmt"`) {
			t.Errorf("%s: import path was wrapped — would not compile:\n%s", path, src)
		}
		// Struct tag must remain literal.
		if !strings.Contains(src, "`json:\"f\"`") {
			t.Errorf("%s: struct tag must survive rewrite:\n%s", path, src)
		}
		if strings.Contains(src, "__cMutString(`json:") {
			t.Errorf("%s: struct tag was wrapped — would not compile:\n%s", path, src)
		}
		// Const initializer must remain literal.
		if !strings.Contains(src, `const Name = "alice"`) {
			t.Errorf("%s: const initializer must survive rewrite:\n%s", path, src)
		}
		if strings.Contains(src, `__cMutString("alice"`) {
			t.Errorf("%s: const initializer was wrapped — would not compile:\n%s", path, src)
		}
		// Empty string returns must survive untouched.
		if !strings.Contains(src, `return ""`) {
			t.Errorf("%s: empty-string return must survive rewrite:\n%s", path, src)
		}
	}
}

func TestStringLiteralRewriteEmitsMutString(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/stringpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{StringLiteral{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for path, src := range rew.Files {
		count := strings.Count(src, "__cMutString(")
		if count != 3 {
			t.Errorf("%s: expected 3 __cMutString calls, got %d:\n%s", path, count, src)
		}
		if !strings.Contains(src, `__cMutString("hello"`) {
			t.Errorf("%s: expected `__cMutString(\"hello\"...)` in Greet:\n%s", path, src)
		}
		if !strings.Contains(src, "__cMutString(`raw`") {
			t.Errorf("%s: expected backtick raw string to be wrapped:\n%s", path, src)
		}
	}
}
