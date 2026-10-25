package operators

import (
	"go/ast"
	"go/printer"
	"go/token"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

// findAll returns "Original→Mutant" for every candidate op finds in the
// fixture, sorted.
func findAll(t *testing.T, fixture string, op mutation.Operator) []string {
	t.Helper()
	pkg, err := source.Load(relDir(t, fixture))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	var got []string
	for _, f := range pkg.Files {
		for _, c := range op.Find(f, pkg.TypesInfo) {
			got = append(got, c.Original+"→"+c.Mutant)
		}
	}
	sort.Strings(got)
	return got
}

func TestNewOperatorsFind(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		op      mutation.Operator
		want    []string
	}{
		{"testdata/floatoppkg", FloatArith{}, []string{"*→/", "*→/", "+→-", "/→*"}},
		{"testdata/floatoppkg", FloatCmpBoundary{}, []string{"<→<=", "<→<=", ">→>=", ">→>="}},
		{"testdata/floatoppkg", FloatCmpNegate{}, []string{"<→>=", "<→>=", "==→!=", ">→<=", ">→<="}},
		{"testdata/chanpkg", ChanOp{}, []string{
			"<-cc→zero", "<-cc→zero", "<-done→zero", "<-in→zero", "<-in→zero", "<-in→zero", "<-in→zero", "<-out→zero",
			"out <- ...→", "p <- ...→",
		}},
		{"testdata/switchpkg", SwitchCase{}, []string{
			"case 1→", "case 1→", "case 1→swap with case 2, 3", "case 2, 3→", "case 2, 3→swap with default",
			"case n > 0→", "case n > 0→swap with case n < 0",
		}},
	} {
		t.Run(tc.op.Name(), func(t *testing.T) {
			if got := findAll(t, tc.fixture, tc.op); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("candidates:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestNewOperatorsRewrite checks the call each operator leaves in the schema.
func TestNewOperatorsRewrite(t *testing.T) {
	for _, tc := range []struct {
		fixture  string
		op       mutation.Operator
		want     []string // substrings of the rewritten source
		dispatch []string // substrings of the dispatcher
	}{
		{"testdata/floatoppkg", FloatArith{}, []string{"__cMutFloat(c, 9, __cMul, 3)"}, []string{"func __cMutFloat[T __cFloat]("}},
		{"testdata/floatoppkg", FloatCmpBoundary{}, []string{"__cMutFloatCmp(a, b, __cLT, 1)", "__cMutFloatCmp(f, 0.5, __cGT, 2)"}, []string{"func __cMutFloatCmp[T __cFloat]("}},
		{"testdata/floatoppkg", FloatCmpNegate{}, []string{"__cMutFloatCmp(a, b, __cLT, 1)", "__cMutFloatCmp(a, b, __cEQ, 2)"}, []string{"func __cMutFloatCmp[T __cFloat]("}},
		{"testdata/chanpkg", ChanOp{}, []string{
			"__cMutCallSkip(func() { out <- __cMutRecv(in, 2) }, 1)",
			"return __cMutRecv(out, 3)",
			"__cMutRecv(done, 4)",
			"case v := <-p:",
			"case <-__cMutRecv(cc, ",
		}, []string{"func __cMutRecv[T any](ch <-chan T, ", "func __cMutCallSkip("}},
		{"testdata/switchpkg", SwitchCase{}, []string{
			// Tagged: each case tests the tag in order and returns its
			// clause's index, a dropped clause is skipped, the default's
			// index is the fallback, and the swaps map indices.
			`switch __cMutSwap(__cMutSwap(func() int { __kanlyTag := n if __cMutKeep(1) { if __kanlyTag == 1 { return 0 } } ` +
				`if __cMutKeep(2) { if __kanlyTag == 2 { return 1 } if __kanlyTag == 3 { return 1 } } return 2 }(), 0, 1, 3), 1, 2, 4) ` +
				`{ case 0: return "one" case 1: return "few" default: return "many" }`,
			// Tagless, no default: case expressions are conditions, and
			// no match returns -1.
			"if __cMutKeep(5) { if n > 0 { return 0 } } if n < 0 { return 1 } return -1 }",
			"switch x.(type) {",
			"switch recover() {",
		}, []string{"func __cMutKeep(", "func __cMutSwap("}},
	} {
		t.Run(tc.op.Name(), func(t *testing.T) {
			pkg, err := source.Load(relDir(t, tc.fixture))
			if err != nil {
				t.Fatalf("source.Load: %v", err)
			}
			rew, err := schema.Rewrite(pkg, []mutation.Operator{tc.op}, nil)
			if err != nil {
				t.Fatalf("schema.Rewrite: %v", err)
			}
			var src strings.Builder
			for _, s := range rew.Files {
				src.WriteString(s)
			}
			flat := flatten(src.String())
			for _, w := range tc.want {
				if !strings.Contains(flat, w) {
					t.Errorf("rewritten source lacks %q:\n%s", w, src.String())
				}
			}
			for _, w := range tc.dispatch {
				if !strings.Contains(rew.Dispatcher, w) {
					t.Errorf("dispatcher lacks %q", w)
				}
			}
		})
	}
}

// TestSwitchCaseRewriteMapsIDsToClauses checks that each mutant ID guards
// the clause it names, whatever order the candidates arrive in.
func TestSwitchCaseRewriteMapsIDsToClauses(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/switchpkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	var tagged *ast.SwitchStmt
	for _, f := range pkg.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			if s, ok := n.(*ast.SwitchStmt); ok && tagged == nil {
				tagged = s
			}
			return true
		})
	}
	var cands []mutation.Candidate
	for _, f := range pkg.Files {
		for _, c := range (SwitchCase{}).Find(f, pkg.TypesInfo) {
			if c.Node == tagged {
				cands = append(cands, c)
			}
		}
	}
	// Reverse the Find order: drop case 1, drop case 2, swap 1↔2, swap 2↔default.
	for i, j := 0, len(cands)-1; i < j; i, j = i+1, j-1 {
		cands[i], cands[j] = cands[j], cands[i]
	}
	var buf strings.Builder
	tag := SwitchCase{}.RewriteGroup(cands, []int{40, 30, 20, 10}).(*ast.SwitchStmt).Tag
	if err := printer.Fprint(&buf, token.NewFileSet(), tag); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{"__cMutKeep(10)", "__cMutKeep(20)", ", 0, 1, 30)", ", 1, 2, 40)"} {
		if !strings.Contains(got, want) {
			t.Errorf("rewritten tag lacks %q: %s", want, got)
		}
	}
	if strings.Index(got, "__cMutKeep(10)") > strings.Index(got, "__cMutKeep(20)") {
		t.Errorf("case 1's drop must guard the first clause: %s", got)
	}
}

// TestFloatComparisonsShareADispatcher guards the pairing of the boundary and
// negate operators: both on one comparison must merge into one call site.
func TestFloatComparisonsShareADispatcher(t *testing.T) {
	pkg, err := source.Load(relDir(t, "testdata/floatoppkg"))
	if err != nil {
		t.Fatalf("source.Load: %v", err)
	}
	rew, err := schema.Rewrite(pkg, []mutation.Operator{FloatCmpBoundary{}, FloatCmpNegate{}}, nil)
	if err != nil {
		t.Fatalf("schema.Rewrite: %v", err)
	}
	for _, src := range rew.Files {
		if flat := flatten(src); !strings.Contains(flat, "__cMutFloatCmp(a, b, __cLT, 1, 5)") {
			t.Errorf("boundary and negate mutants of a < b not merged:\n%s", src)
		}
	}
}

// TestOperatorsAreRegistered checks the registry holds every operator once,
// each with a name and a dispatcher key.
func TestOperatorsAreRegistered(t *testing.T) {
	var names []string
	for _, op := range mutation.Operators() {
		if op.DispatcherKey() == "" {
			t.Errorf("%s has no dispatcher key", op.Name())
		}
		names = append(names, op.Name())
	}
	want := []string{
		"int_arith", "int_cmp_boundary", "int_cmp_negate", "bool_logic", "bool_not", "err_return_nil",
		"call_delete", "string_literal", "slice_index", "slice_range", "inc_dec", "int_compound_assign",
		"int_bitwise", "int_literal", "return_zero", "bool_literal", "struct_field_zero",
		"float_arith", "float_cmp_boundary", "float_cmp_negate", "chan_op", "switch_case",
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("registered:\n got %q\nwant %q", names, want)
	}
}

// flatten joins src into one line without comments: the printer breaks
// lines, and flushes comments, around the unpositioned nodes a rewrite adds.
func flatten(src string) string {
	var code []string
	for _, line := range strings.Split(src, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "//") {
			code = append(code, line)
		}
	}
	return strings.Join(strings.Fields(strings.Join(code, " ")), " ")
}
