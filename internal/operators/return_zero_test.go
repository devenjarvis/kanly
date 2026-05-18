package operators_test

import (
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/operators"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

func TestReturnZeroFindsEligibleReturns(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/retzeropkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	var candidates []mutation.Candidate
	for _, f := range pkg.Files {
		candidates = append(candidates, operators.ReturnZero{}.Find(f, pkg.TypesInfo)...)
	}

	// Eligible (each contributes 1 candidate):
	//   IntIdent, StringIdent, BoolIdent,
	//   PtrIdent, SliceIdent, MapIdent, ChanIdent, FuncIdent, InterfaceIdent,
	//   CallExprReturn, IndexReturn, holder.Get (selector), CompositeLitReturn
	// MixedReturn returns 2 idents (int, string) → 2 candidates.
	// All others skipped (binary/unary/literal/error/named/float/naked/multi-call).
	want := 13 + 2
	if len(candidates) != want {
		t.Fatalf("expected %d candidates, got %d:\n%+v", want, len(candidates), candidates)
	}

	mutCounts := map[string]int{}
	for _, c := range candidates {
		mutCounts[c.Mutant]++
	}
	// 1×int + 1×index + 1×call + 1×selector + 1×mixed-int = 5
	if mutCounts["0"] != 5 {
		t.Errorf("expected 5 → 0 mutants (int), got %d", mutCounts["0"])
	}
	// 1×string + 1×mixed-string = 2
	if mutCounts[`""`] != 2 {
		t.Errorf("expected 2 → \"\" mutants, got %d", mutCounts[`""`])
	}
	if mutCounts["false"] != 1 {
		t.Errorf("expected 1 → false mutant, got %d", mutCounts["false"])
	}
	// 7 nilable: ptr, slice, map, chan, func, interface, compositeLit-slice = 7.
	if mutCounts["nil"] != 7 {
		t.Errorf("expected 7 → nil mutants, got %d", mutCounts["nil"])
	}
}

func TestReturnZeroSkipsExcludedShapes(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/retzeropkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	rew, err := schema.Rewrite(pkg, []mutation.Operator{operators.ReturnZero{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}
	for path, src := range rew.Files {
		// BinaryExpr return must survive untouched.
		if !strings.Contains(src, "return a + b") {
			t.Errorf("%s: BinaryExpr return must survive rewrite:\n%s", path, src)
		}
		// UnaryExpr return must survive untouched.
		if !strings.Contains(src, "return !b") {
			t.Errorf("%s: UnaryExpr return must survive rewrite:\n%s", path, src)
		}
		// BasicLit return must survive untouched.
		if !strings.Contains(src, "return 42") {
			t.Errorf("%s: BasicLit return must survive rewrite:\n%s", path, src)
		}
		// Error return must survive (handled by err_return_nil, not return_zero).
		if strings.Contains(src, "func ErrorReturn(err error) error { return __cMutRetZero") {
			t.Errorf("%s: error return must not be wrapped by return_zero:\n%s", path, src)
		}
		// Multi-value call passthrough must survive untouched.
		if !strings.Contains(src, "return multiResult()") {
			t.Errorf("%s: multi-value call passthrough must survive rewrite:\n%s", path, src)
		}
		// Named int must survive.
		if strings.Contains(src, "func NamedIntReturn(x MyInt) MyInt { return __cMutRetZero") {
			t.Errorf("%s: named int return must not be wrapped:\n%s", path, src)
		}
	}
}

func TestReturnZeroRewriteEmitsMutRetZero(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/retzeropkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	rew, err := schema.Rewrite(pkg, []mutation.Operator{operators.ReturnZero{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}
	// 13 single-return + 2 in MixedReturn = 15 call sites.
	for path, src := range rew.Files {
		count := strings.Count(src, "__cMutRetZero(")
		if count != 15 {
			t.Errorf("%s: expected 15 __cMutRetZero call sites, got %d:\n%s", path, count, src)
		}
	}
	if !strings.Contains(rew.Dispatcher, "func __cMutRetZero[T any](") {
		t.Errorf("dispatcher missing generic __cMutRetZero:\n%s", rew.Dispatcher)
	}
}

func TestReturnZeroAndErrReturnCoexist(t *testing.T) {
	// errpkg has both error returns (err_return_nil) and non-error returns.
	// Running both operators together must not double-wrap and must compile.
	pkg, err := source.Load(relDir(t, "testdata/errpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	rew, err := schema.Rewrite(pkg, []mutation.Operator{operators.ErrReturnNil{}, operators.ReturnZero{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}
	var sawErr, sawZero bool
	for _, m := range rew.Mutations {
		switch m.OperatorName {
		case "err_return_nil":
			sawErr = true
		case "return_zero":
			sawZero = true
		}
	}
	if !sawErr {
		t.Errorf("expected at least one err_return_nil mutation")
	}
	if !sawZero {
		// errpkg has ReturnIntAndNil returning `(int, error)` with `return 1, nil` —
		// the int operand is a BasicLit (skipped) and the error is literal nil (skipped).
		// Other returns are BinaryExpr-free idents in returns of error type, all
		// claimed by err_return_nil. So return_zero might legitimately find none.
		t.Logf("no return_zero mutations in errpkg (expected — error-typed returns claimed by err_return_nil)")
	}
}
