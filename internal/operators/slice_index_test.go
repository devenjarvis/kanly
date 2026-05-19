package operators

import (
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

func TestSliceIndexEmitsTwoCandidatesPerSite(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/sliceidxpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, SliceIndex{}.Find(f, pkg.TypesInfo)...)
	}

	// Mutable sites: SliceIdx, ArrayIdx, StringByte, MapIntKey, MapNamedKey → 5 × 2 = 10.
	if len(candidates) != 10 {
		t.Fatalf("expected 10 candidates (5 sites × 2 mutations), got %d", len(candidates))
	}

	var plus, minus int
	for _, c := range candidates {
		switch c.Mutant {
		case "+1":
			plus++
		case "-1":
			minus++
		default:
			t.Errorf("unexpected Mutant=%q", c.Mutant)
		}
	}
	if plus != 5 || minus != 5 {
		t.Errorf("expected 5 +1 and 5 -1 candidates, got plus=%d minus=%d", plus, minus)
	}
}

func TestSliceIndexSkipsExcludedKeys(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/sliceidxpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{SliceIndex{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for path, src := range rew.Files {
		// Non-int map key must remain untouched.
		if !strings.Contains(src, `m["key"]`) {
			t.Errorf("%s: string-keyed map index must survive rewrite:\n%s", path, src)
		}
		// Constant index must remain untouched.
		if !strings.Contains(src, "s[0]") {
			t.Errorf("%s: constant slice index must survive rewrite:\n%s", path, src)
		}
		if !strings.Contains(src, "s[1+1]") {
			t.Errorf("%s: constant arithmetic index must survive rewrite:\n%s", path, src)
		}
	}
}

func TestSliceIndexRewriteEmitsMutIdx(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/sliceidxpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{SliceIndex{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for path, src := range rew.Files {
		// Five mutable sites (the named-int MapNamedKey is now mutated). Two
		// candidates per site share one call site via (node, dispatcherKey).
		count := strings.Count(src, "__cMutIdx(")
		if count != 5 {
			t.Errorf("%s: expected 5 __cMutIdx calls, got %d:\n%s", path, count, src)
		}
	}
}
