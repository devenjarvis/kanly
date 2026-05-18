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
	// Sized/named/const/array literals must all be skipped.
	want := 5 + 2 + 3
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
	if gotOriginals["0"] != 2 {
		t.Errorf("expected 2 mutants for `0`, got %d", gotOriginals["0"])
	}
	if gotOriginals["1"] != 3 {
		t.Errorf("expected 3 mutants for `1`, got %d", gotOriginals["1"])
	}
	if gotOriginals["99"] != 0 {
		t.Errorf("expected sized-int literal `99` to be skipped, got %d", gotOriginals["99"])
	}
	if gotOriginals["7"] != 0 {
		t.Errorf("expected named-int literal `7` to be skipped, got %d", gotOriginals["7"])
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
	// Dispatcher must include each mutant value as a case-return literal.
	for _, want := range []string{"return 0", "return 1", "return -42", "return 43", "return 41", "return -1", "return 2"} {
		if !strings.Contains(rew.Dispatcher, want) {
			t.Errorf("dispatcher missing %q:\n%s", want, rew.Dispatcher)
		}
	}
}
