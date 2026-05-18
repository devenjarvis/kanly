package operators_test

import (
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/operators"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

func TestIntBitwiseFindsAllSixOps(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/bitwisepkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}

	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, operators.IntBitwise{}.Find(f, pkg.TypesInfo)...)
	}

	want := map[string]string{
		"&":  "|",
		"|":  "&",
		"^":  "&",
		"<<": ">>",
		">>": "<<",
		"&^": "&",
	}
	if len(candidates) != len(want) {
		t.Fatalf("expected %d candidates, got %d", len(want), len(candidates))
	}
	got := map[string]string{}
	for _, c := range candidates {
		got[c.Original] = c.Mutant
	}
	for op, mut := range want {
		if got[op] != mut {
			t.Errorf("%s: want mutant %s, got %s", op, mut, got[op])
		}
	}
}

func TestIntBitwiseSkipsSizedAndUintTypes(t *testing.T) {
	// bitwisepkg also contains uint & uint and int32 << int32 forms; the strict
	// type policy must skip them. Total candidates from Find should still be 6
	// (the plain-int functions only), not 8.
	pkg, err := source.Load(relDir(t, "testdata/bitwisepkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	var n int
	for _, f := range pkg.Files {
		n += len(operators.IntBitwise{}.Find(f, pkg.TypesInfo))
	}
	if n != 6 {
		t.Errorf("expected 6 candidates (only plain int ops), got %d", n)
	}
}

func TestIntBitwiseRewriteEmitsMutIntBit(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/bitwisepkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	rew, err := schema.Rewrite(pkg, []mutation.Operator{operators.IntBitwise{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}
	for path, src := range rew.Files {
		count := strings.Count(src, "__cMutIntBit(")
		if count != 6 {
			t.Errorf("%s: expected 6 __cMutIntBit calls, got %d:\n%s", path, count, src)
		}
		// Sized types must not be wrapped.
		if strings.Contains(src, "func AndUint(a, b uint) uint { return __cMutIntBit") {
			t.Errorf("%s: AndUint should not be wrapped (uint not int)", path)
		}
	}
	if !strings.Contains(rew.Dispatcher, "func __cMutIntBit(") {
		t.Errorf("dispatcher missing __cMutIntBit:\n%s", rew.Dispatcher)
	}
}
