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

	// 6 plain-int ops (And, Or, Xor, Shl, Shr, AndNot) plus the sized variants
	// AndUint (uint & uint) and ShlInt32 (int32 << int32) — 8 total candidates.
	if len(candidates) != 8 {
		t.Fatalf("expected 8 candidates, got %d", len(candidates))
	}
	wantMutant := map[string]string{
		"&":  "|",
		"|":  "&",
		"^":  "&",
		"<<": ">>",
		">>": "<<",
		"&^": "&",
	}
	seen := map[string]int{}
	for _, c := range candidates {
		want, ok := wantMutant[c.Original]
		if !ok {
			t.Errorf("unexpected Original %q", c.Original)
			continue
		}
		if c.Mutant != want {
			t.Errorf("%s: want mutant %s, got %s", c.Original, want, c.Mutant)
		}
		seen[c.Original]++
	}
	// & appears twice (And int + AndUint uint); << appears twice (Shl + ShlInt32).
	if seen["&"] != 2 {
		t.Errorf("& count: want 2 (And + AndUint), got %d", seen["&"])
	}
	if seen["<<"] != 2 {
		t.Errorf("<< count: want 2 (Shl + ShlInt32), got %d", seen["<<"])
	}
	for _, op := range []string{"|", "^", ">>", "&^"} {
		if seen[op] != 1 {
			t.Errorf("%s count: want 1, got %d", op, seen[op])
		}
	}
}

func TestIntBitwiseAcceptsSizedAndUintTypes(t *testing.T) {
	// bitwisepkg contains uint & uint (AndUint) and int32 << int32 (ShlInt32).
	// With the underlying-integer policy these are now mutated alongside plain
	// int — total candidates from Find should be 8.
	pkg, err := source.Load(relDir(t, "testdata/bitwisepkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	var n int
	for _, f := range pkg.Files {
		n += len(operators.IntBitwise{}.Find(f, pkg.TypesInfo))
	}
	if n != 8 {
		t.Errorf("expected 8 candidates (plain + sized int ops), got %d", n)
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
		if count != 8 {
			t.Errorf("%s: expected 8 __cMutIntBit calls, got %d:\n%s", path, count, src)
		}
		// Sized types are now wrapped — Go's generic inference picks the type from operands.
		if !strings.Contains(src, "func AndUint(a, b uint) uint { return __cMutIntBit") {
			t.Errorf("%s: AndUint should be wrapped via generic __cMutIntBit", path)
		}
	}
	if !strings.Contains(rew.Dispatcher, "func __cMutIntBit[T __cInteger](") {
		t.Errorf("dispatcher missing generic __cMutIntBit:\n%s", rew.Dispatcher)
	}
}
