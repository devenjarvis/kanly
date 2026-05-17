package operators

import (
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

func TestBoolLogicRejectsNamedBool(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/namedboolpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, BoolLogic{}.Find(f, pkg.TypesInfo)...)
	}

	if len(candidates) != 0 {
		t.Errorf("BoolLogic.Find: expected 0 candidates for named bool type, got %d: %v", len(candidates), candidates)
	}
}

func TestBoolLogicFindsBothSwaps(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/boolpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, BoolLogic{}.Find(f, pkg.TypesInfo)...)
	}

	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %v", len(candidates), candidates)
	}

	found := map[string]bool{}
	for _, c := range candidates {
		found[c.Original+"→"+c.Mutant] = true
	}
	if !found["&&→||"] {
		t.Errorf("expected &&→|| candidate, got %v", candidates)
	}
	if !found["||→&&"] {
		t.Errorf("expected ||→&& candidate, got %v", candidates)
	}
}

func TestBoolNotRejectsNamedBool(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/namedboolpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, BoolNot{}.Find(f, pkg.TypesInfo)...)
	}

	if len(candidates) != 0 {
		t.Errorf("BoolNot.Find: expected 0 candidates for named bool type, got %d: %v", len(candidates), candidates)
	}
}

func TestBoolNotFindsOneRemoval(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/boolpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, BoolNot{}.Find(f, pkg.TypesInfo)...)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d: %v", len(candidates), candidates)
	}
	if candidates[0].Original != "!" || candidates[0].Mutant != "" {
		t.Errorf("expected !→\"\", got %q→%q", candidates[0].Original, candidates[0].Mutant)
	}
}

func TestBoolNotRewriteEmitsMutNot(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/boolpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{BoolNot{}})
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for path, src := range rew.Files {
		if !strings.Contains(src, "__cMutNot(") {
			t.Errorf("%s: expected __cMutNot call:\n%s", path, src)
		}
		if strings.Contains(src, "!b1") {
			t.Errorf("%s: expected !b1 to be rewritten, but still present:\n%s", path, src)
		}
	}
}

func TestBoolLogicRewriteWrapsInClosures(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/boolpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{BoolLogic{}})
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for path, src := range rew.Files {
		count := strings.Count(src, "__cMutBool(")
		if count != 2 {
			t.Errorf("%s: expected 2 __cMutBool calls, got %d:\n%s", path, count, src)
		}
		if !strings.Contains(src, "func() bool") {
			t.Errorf("%s: expected func() bool closures:\n%s", path, src)
		}
		if !strings.Contains(src, "__cAnd") {
			t.Errorf("%s: expected __cAnd constant:\n%s", path, src)
		}
		if !strings.Contains(src, "__cOr") {
			t.Errorf("%s: expected __cOr constant:\n%s", path, src)
		}
	}
}
