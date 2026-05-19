package operators_test

import (
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/operators"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

func TestSliceRangeFindsAllBoundCombinations(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/slicerangepkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	op := operators.SliceRange{}
	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, op.Find(f, pkg.TypesInfo)...)
	}

	// TwoIndex: 4 (2 bounds × 2 muts), LowOnly: 2, HighOnly: 2,
	// FullBounds: 6 (3 bounds × 2 muts), Int32Bounds: 2 = 16 total.
	// AllNil and ConstBounds emit 0.
	if len(candidates) != 16 {
		t.Fatalf("expected 16 candidates, got %d", len(candidates))
	}

	var plus, minus int
	bounds := map[string]int{}
	for _, c := range candidates {
		switch c.Mutant {
		case "+1":
			plus++
		case "-1":
			minus++
		default:
			t.Errorf("unexpected Mutant=%q", c.Mutant)
		}
		bounds[c.Original]++
	}
	if plus != 8 || minus != 8 {
		t.Errorf("expected 8 +1 and 8 -1 candidates, got plus=%d minus=%d", plus, minus)
	}
	// Each bound kind appears N times where N matches the count of fixture occurrences:
	// lo: TwoIndex, LowOnly, FullBounds → 3 × 2 mutants = 6
	// hi: TwoIndex, HighOnly, FullBounds, Int32Bounds → 4 × 2 = 8
	// max: FullBounds → 1 × 2 = 2
	if bounds["lo"] != 6 {
		t.Errorf("lo bound count: want 6, got %d", bounds["lo"])
	}
	if bounds["hi"] != 8 {
		t.Errorf("hi bound count: want 8, got %d", bounds["hi"])
	}
	if bounds["max"] != 2 {
		t.Errorf("max bound count: want 2, got %d", bounds["max"])
	}
}

func TestSliceRangeSkipsConstAndNilBounds(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/slicerangepkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	rew, err := schema.Rewrite(pkg, []mutation.Operator{operators.SliceRange{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}
	for path, src := range rew.Files {
		// Constant-bounded form must remain untouched.
		if !strings.Contains(src, "s[0:3]") {
			t.Errorf("%s: constant-bounded slice must survive rewrite:\n%s", path, src)
		}
		// Open-ended s[:] must remain untouched.
		if !strings.Contains(src, "s[:]") {
			t.Errorf("%s: open-ended slice must survive rewrite:\n%s", path, src)
		}
	}
}

func TestSliceRangeRewriteEmitsMutIdx(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/slicerangepkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	rew, err := schema.Rewrite(pkg, []mutation.Operator{operators.SliceRange{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}
	for path, src := range rew.Files {
		// 8 distinct bound sites: TwoIndex(2) + LowOnly(1) + HighOnly(1) + FullBounds(3) + Int32Bounds(1).
		count := strings.Count(src, "__cMutIdx(")
		if count != 8 {
			t.Errorf("%s: expected 8 __cMutIdx calls, got %d:\n%s", path, count, src)
		}
	}
}

func TestSliceRangeAndIndexShareDispatcher(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/slicerangepkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	rew, err := schema.Rewrite(pkg, []mutation.Operator{
		operators.SliceIndex{},
		operators.SliceRange{},
	}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}
	// Exactly one __cMutIdx function definition in the dispatcher.
	defCount := strings.Count(rew.Dispatcher, "func __cMutIdx[T __cInteger]")
	if defCount != 1 {
		t.Errorf("expected exactly 1 __cMutIdx definition, got %d:\n%s", defCount, rew.Dispatcher)
	}
}
