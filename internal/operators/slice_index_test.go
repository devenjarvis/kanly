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

	// Mutable sites: SliceIdx, ArrayIdx, StringByte, MapIntKey → 4 × 2 = 8.
	if len(candidates) != 8 {
		t.Fatalf("expected 8 candidates (4 sites × 2 mutations), got %d", len(candidates))
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
	if plus != 4 || minus != 4 {
		t.Errorf("expected 4 +1 and 4 -1 candidates, got plus=%d minus=%d", plus, minus)
	}
}

func TestSliceIndexSkipsExcludedKeys(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/sliceidxpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{SliceIndex{}})
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for path, src := range rew.Files {
		// Non-int map key must remain untouched.
		if !strings.Contains(src, `m["key"]`) {
			t.Errorf("%s: string-keyed map index must survive rewrite:\n%s", path, src)
		}
		// Named-int key must remain untouched.
		if !strings.Contains(src, "m[k]") {
			t.Errorf("%s: named-int map index must survive rewrite:\n%s", path, src)
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

	rew, err := schema.Rewrite(pkg, []mutation.Operator{SliceIndex{}})
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}

	for path, src := range rew.Files {
		// Four mutable sites, two candidates per site, but co-located mutations
		// share one call site via the (node, dispatcherKey) grouping → 4 calls.
		count := strings.Count(src, "__cMutIdx(")
		if count != 4 {
			t.Errorf("%s: expected 4 __cMutIdx calls, got %d:\n%s", path, count, src)
		}
	}
}
