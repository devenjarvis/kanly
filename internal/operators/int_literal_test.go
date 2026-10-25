package operators_test

import (
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/operators"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

func TestIntLiteralFindsMagicNumberMutants(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/intlitpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, operators.IntLiteral{}.Find(f, pkg.TypesInfo)...)
	}

	// Per literal:
	//   42: {0, 1, -42, 43, 41}                → 5
	//    0: {1, -1}                            → 2
	//    1: {0, -1, 2}                         → 3
	//   99 (int64): {0, 1, -99, 100, 98}       → 5
	//    0 (uint8): {1}                        → 1
	//  127 (int8): {0, 1, -127, 126}           → 4
	//    7 (MyInt): {0, 1, -7, 8, 6}           → 5
	// Const/array/shift-count/float-context literals must all be skipped.
	want := 5 + 2 + 3 + 5 + 1 + 4 + 5
	if len(candidates) != want {
		t.Fatalf("expected %d candidates, got %d:\n%+v", want, len(candidates), candidates)
	}

	gotOriginals := map[string]int{}
	for _, c := range candidates {
		gotOriginals[c.Original]++
	}
	if gotOriginals["42"] != 5 {
		t.Errorf("expected 5 mutants for `42`, got %d", gotOriginals["42"])
	}
	if gotOriginals["0"] != 3 {
		t.Errorf("expected 2 mutants for int `0` and 1 for uint8 `0`, got %d", gotOriginals["0"])
	}
	if gotOriginals["1"] != 3 {
		t.Errorf("expected 3 mutants for `1`, got %d", gotOriginals["1"])
	}
	if gotOriginals["99"] != 5 {
		t.Errorf("expected 5 mutants for sized-int literal `99`, got %d", gotOriginals["99"])
	}
	if gotOriginals["7"] != 5 {
		t.Errorf("expected 5 mutants for named-int literal `7`, got %d", gotOriginals["7"])
	}
	for _, c := range candidates {
		if c.Original == "127" && c.Mutant == "128" {
			t.Errorf("int8 literal 127 must not mutate to 128, which overflows")
		}
		if c.Original == "3" || c.Original == "2" {
			t.Errorf("shift-count or float-context literal %s must be skipped", c.Original)
		}
	}
}

func TestIntLiteralSkipsForbiddenContexts(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/intlitpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	rew, err := schema.Rewrite(pkg, []mutation.Operator{operators.IntLiteral{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}
	for path, src := range rew.Files {
		// Const-decl initializer must remain literal.
		if !strings.Contains(src, "const ConstAnswer = 42") {
			t.Errorf("%s: const initializer must survive rewrite:\n%s", path, src)
		}
		if strings.Contains(src, "ConstAnswer = __cMutIntLit") {
			t.Errorf("%s: const initializer was wrapped — would not compile:\n%s", path, src)
		}
		// Array length must remain literal.
		if !strings.Contains(src, "[4]int{") {
			t.Errorf("%s: array length must survive rewrite:\n%s", path, src)
		}
		if strings.Contains(src, "[__cMutIntLit(4") {
			t.Errorf("%s: array length was wrapped — would not compile:\n%s", path, src)
		}
	}
}

func TestIntLiteralRewriteEmitsMutIntLit(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/intlitpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	rew, err := schema.Rewrite(pkg, []mutation.Operator{operators.IntLiteral{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}
	// Three literals (42, 0, 1) become three call sites; mutant-count totals don't
	// matter at the rewrite layer (multiple candidates collapse per node).
	for path, src := range rew.Files {
		count := strings.Count(src, "__cMutIntLit(")
		if count != 3 {
			t.Errorf("%s: expected 3 __cMutIntLit call sites, got %d:\n%s", path, count, src)
		}
	}
	if !strings.Contains(rew.Dispatcher, "func __cMutIntLit(") {
		t.Errorf("dispatcher missing __cMutIntLit:\n%s", rew.Dispatcher)
	}
	// A sized or named literal becomes a context-typed sum with one term per
	// mutant value; a zero mutant needs no term.
	for path, src := range rew.Files {
		if !strings.Contains(src, "return (99*(1>>") || !strings.Contains(src, "-99*(1>>__cMutLitOn(") {
			t.Errorf("%s: sized literal 99 not rewritten to a context-typed sum:\n%s", path, src)
		}
		if strings.Contains(src, " 0*(1>>") {
			t.Errorf("%s: zero mutant emitted a term:\n%s", path, src)
		}
	}
	if !strings.Contains(rew.Dispatcher, "func __cMutLitOn(") {
		t.Errorf("dispatcher missing __cMutLitOn:\n%s", rew.Dispatcher)
	}
	// Dispatcher must include each mutant value as a case-return literal.
	for _, want := range []string{"return 0", "return 1", "return -42", "return 43", "return 41", "return -1", "return 2"} {
		if !strings.Contains(rew.Dispatcher, want) {
			t.Errorf("dispatcher missing %q:\n%s", want, rew.Dispatcher)
		}
	}
}
